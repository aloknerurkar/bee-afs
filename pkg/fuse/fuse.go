package fs

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	bf "github.com/aloknerurkar/bee-afs/pkg/file"
	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/ethersphere/bee/pkg/file/joiner"
	"github.com/ethersphere/bee/pkg/file/splitter"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
	logger "github.com/ipfs/go-log/v2"
)

func init() {
	gob.Register(FsMetadata{})
}

var log = logger.Logger("fuse/beeFs")

func trace(start time.Time, errc *int, vals ...interface{}) {
	pc, _, _, ok := runtime.Caller(1)
	name := "<UNKNOWN>"
	if ok {
		fn := runtime.FuncForPC(pc)
		name = fn.Name()
	}
	args := "("
	for idx, v := range vals {
		switch v.(type) {
		case string:
			args += v.(string)
		case int:
			args += strconv.Itoa(v.(int))
		case int64:
			args += strconv.FormatInt(v.(int64), 10)
		case uint64:
			args += strconv.FormatUint(v.(uint64), 10)
		}
		if idx != len(vals)-1 {
			args += ", "
		}
	}
	args += ")"
	log.Debugf("%s took %s args: %s result: %d", name,
		time.Since(start).String(), args, *errc)
}

type fsNode struct {
	id       string
	stat     fuse.Stat_t
	xatr     map[string][]byte
	children []string
	opencnt  int
	data     *bf.BeeFile
	commit   func() error
}

func (f *fsNode) isDir() bool {
	if f.stat.Mode&fuse.S_IFDIR > 0 {
		return true
	}
	return false
}

func (f *fsNode) Close() error {
	return f.commit()
}

func (f *fsNode) clone(newpath string) *fsNode {
	return nil
}

type FsMetadata struct {
	Stat     fuse.Stat_t
	Xatr     map[string][]byte
	Children []string
}

type metadataReader struct {
	bytes.Buffer
}

func (m *metadataReader) Close() error {
	m.Reset()
	return nil
}

func (f *fsNode) metadata() (io.ReadCloser, int64, error) {
	md := FsMetadata{
		Stat: f.stat,
		Xatr: f.xatr,
	}
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(md)
	if err != nil {
		return nil, 0, err
	}
	return &metadataReader{buf}, int64(buf.Len()), nil
}

func fromMetadata(reader io.Reader) (FsMetadata, error) {
	md := FsMetadata{}
	err := gob.NewDecoder(reader).Decode(&md)
	if err != nil {
		return FsMetadata{}, fmt.Errorf("failed decoding blob %w", err)
	}
	return md, nil
}

type BeeFs struct {
	fuse.FileSystemBase
	lock    sync.Mutex
	ino     uint64
	openmap map[uint64]*fsNode
	pin     bool
	encrypt bool
	store   store.PutGetter
	id      string
	lk      lookuper.Lookuper
	pb      publisher.Publisher
}

type Option func(*BeeFs)

func WithPin(val bool) Option {
	return func(r *BeeFs) {
		r.pin = val
	}
}

func WithEncryption(val bool) Option {
	return func(r *BeeFs) {
		r.encrypt = val
	}
}

func New(st store.PutGetter, opts ...Option) (*BeeFs, error) {
	b := &BeeFs{}
	for _, opt := range opts {
		opt(b)
	}
	b.openmap = map[uint64]*fsNode{}
	return b, nil
}

func (b *BeeFs) Init() {
	defer trace(time.Now(), new(int))
}

func (b *BeeFs) Destroy() {
	defer trace(time.Now(), new(int))
}

func (b *BeeFs) Mknod(path string, mode uint32, dev uint64) (errc int) {
	defer trace(time.Now(), &errc, path, mode, dev)
	defer b.synchronize()()

	return b.makeNode(path, mode, dev, nil)
}

func (b *BeeFs) Mkdir(path string, mode uint32) (errc int) {
	defer trace(time.Now(), &errc, path, mode)
	defer b.synchronize()()

	return b.makeNode(path, fuse.S_IFDIR|(mode&07777), 0, nil)
}

func (b *BeeFs) Unlink(path string) (errc int) {
	defer trace(time.Now(), &errc, path)
	defer b.synchronize()()

	return b.removeNode(path, false)
}

func (b *BeeFs) Rmdir(path string) (errc int) {
	defer trace(time.Now(), &errc, path)
	defer b.synchronize()()

	return b.removeNode(path, true)
}

func (b *BeeFs) Link(oldpath string, newpath string) (errc int) {
	defer trace(time.Now(), &errc, oldpath, newpath)
	defer b.synchronize()()

	oldnode := b.lookupNode(oldpath)
	if nil == oldnode {
		return -fuse.ENOENT
	}
	defer oldnode.Close()

	newnode := b.lookupNode(newpath)
	if nil != newnode {
		return -fuse.EEXIST
	}

	newprnt := b.lookupNode(filepath.Dir(newpath))
	if nil == newprnt {
		return -fuse.ENOENT
	}
	defer newprnt.Close()

	newnode = oldnode.clone(newpath)
	if err := newnode.Close(); err != nil {
		return -fuse.EIO
	}

	oldnode.stat.Nlink++
	newprnt.children = append(newprnt.children, filepath.Base(newpath))
	tmsp := fuse.Now()
	oldnode.stat.Ctim = tmsp
	newprnt.stat.Ctim = tmsp
	newprnt.stat.Mtim = tmsp
	return 0
}

func (b *BeeFs) Symlink(target string, newpath string) (errc int) {
	defer trace(time.Now(), &errc, target, newpath)
	defer b.synchronize()()

	return b.makeNode(newpath, fuse.S_IFLNK|00777, 0, []byte(target))
}

func (b *BeeFs) Readlink(path string) (errc int, target string) {
	defer trace(time.Now(), &errc, path)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT, ""
	}
	if fuse.S_IFLNK != node.stat.Mode&fuse.S_IFMT {
		return -fuse.EINVAL, ""
	}
	linkBuf := make([]byte, 1024)
	_, err := node.data.ReadAt(linkBuf, 0)
	if err != nil {
		return -fuse.EIO, ""
	}
	return 0, string(linkBuf)
}

func (b *BeeFs) Rename(oldpath string, newpath string) (errc int) {
	defer trace(time.Now(), &errc, oldpath, newpath)
	defer b.synchronize()()

	if newpath == oldpath {
		return 0
	}

	oldnode := b.lookupNode(oldpath)
	if nil == oldnode {
		return -fuse.ENOENT
	}
	oldprnt := b.lookupNode(filepath.Dir(oldpath))
	if nil == oldprnt {
		return -fuse.ENOENT
	}

	if oldnode.isDir() && strings.Contains(newpath, filepath.Dir(oldpath)) {
		// directory loop creation
		return -fuse.EINVAL
	}

	newnode := b.lookupNode(newpath)
	if nil != newnode {
		errc = b.removeNode(newpath, fuse.S_IFDIR == oldnode.stat.Mode&fuse.S_IFMT)
		if 0 != errc {
			return errc
		}
	}
	newprnt := b.lookupNode(filepath.Dir(newpath))
	if nil == newprnt {
		return -fuse.ENOENT
	}
	if nil == newnode {
		newprnt.children = append(newprnt.children, filepath.Base(newpath))
	}
	for idx, chld := range oldprnt.children {
		if chld == filepath.Base(oldpath) {
			oldprnt.children = append(oldprnt.children[:idx], oldprnt.children[idx+1:]...)
			break
		}
	}

	newnode = oldnode.clone(newpath)
	if err := newnode.Close(); err != nil {
		return -fuse.EIO
	}

	return 0
}

func (b *BeeFs) Chmod(path string, mode uint32) (errc int) {
	defer trace(time.Now(), &errc, path, mode)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	node.stat.Mode = (node.stat.Mode & fuse.S_IFMT) | mode&07777
	node.stat.Ctim = fuse.Now()
	return 0
}

func (b *BeeFs) Chown(path string, uid uint32, gid uint32) (errc int) {
	defer trace(time.Now(), &errc, path, uid, gid)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	if ^uint32(0) != uid {
		node.stat.Uid = uid
	}
	if ^uint32(0) != gid {
		node.stat.Gid = gid
	}
	node.stat.Ctim = fuse.Now()
	return 0
}

func (b *BeeFs) Utimens(path string, tmsp []fuse.Timespec) (errc int) {
	defer trace(time.Now(), &errc, path)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	node.stat.Ctim = fuse.Now()
	if nil == tmsp {
		tmsp0 := node.stat.Ctim
		tmsa := [2]fuse.Timespec{tmsp0, tmsp0}
		tmsp = tmsa[:]
	}
	node.stat.Atim = tmsp[0]
	node.stat.Mtim = tmsp[1]
	return 0
}

func (b *BeeFs) Open(path string, flags int) (errc int, fh uint64) {
	defer trace(time.Now(), &errc, path, flags)
	defer b.synchronize()()

	return b.openNode(path, false)
}

func (b *BeeFs) Getattr(path string, stat *fuse.Stat_t, fh uint64) (errc int) {
	defer trace(time.Now(), &errc, path, fh)
	defer b.synchronize()()

	node := b.getNode(path, fh)
	if nil == node {
		return -fuse.ENOENT
	}
	*stat = node.stat
	return 0
}

func (b *BeeFs) Truncate(path string, size int64, fh uint64) (errc int) {
	defer trace(time.Now(), &errc, path, size, fh)
	defer b.synchronize()()

	node := b.getNode(path, fh)
	if nil == node {
		return -fuse.ENOENT
	}
	err := node.data.Truncate(size)
	if err != nil {
		return -fuse.EIO
	}
	node.stat.Size = size
	tmsp := fuse.Now()
	node.stat.Ctim = tmsp
	node.stat.Mtim = tmsp

	return 0
}

func (b *BeeFs) Read(path string, buff []byte, ofst int64, fh uint64) (n int) {
	defer trace(time.Now(), &n, path, ofst, fh)
	defer b.synchronize()()

	node := b.getNode(path, fh)
	if nil == node {
		return -fuse.ENOENT
	}
	var err error
	if cap(buff)-len(buff) > 1024 {
		dBuf := make([]byte, len(buff))
		n, err = node.data.ReadAt(dBuf, ofst)
		if err == nil {
			copy(buff, dBuf)
		}
	} else {
		n, err = node.data.ReadAt(buff, ofst)
	}
	if err != nil {
		log.Error("failed read", err)
		return -fuse.EIO
	}
	node.stat.Atim = fuse.Now()
	return n
}

func (b *BeeFs) Write(path string, buff []byte, ofst int64, fh uint64) (n int) {
	defer trace(time.Now(), &n, path, ofst, fh)
	defer b.synchronize()()

	node := b.getNode(path, fh)
	if nil == node {
		return -fuse.ENOENT
	}
	endofst := ofst + int64(len(buff))
	if endofst > node.stat.Size {
		node.stat.Size = endofst
	}
	var err error
	n, err = node.data.WriteAt(buff, ofst)
	if err != nil {
		return -fuse.EIO
	}
	tmsp := fuse.Now()
	node.stat.Ctim = tmsp
	node.stat.Mtim = tmsp
	return n
}

func (b *BeeFs) Release(path string, fh uint64) (errc int) {
	defer trace(time.Now(), &errc, path, fh)
	defer b.synchronize()()

	return b.closeNode(fh)
}

func (b *BeeFs) Opendir(path string) (errc int, fh uint64) {
	defer trace(time.Now(), &errc, path)
	defer b.synchronize()()

	return b.openNode(path, true)
}

func (b *BeeFs) Readdir(
	path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64,
) (errc int) {
	defer trace(time.Now(), &errc, path, ofst, fh)
	defer b.synchronize()()

	node := b.getNode(path, fh)
	if nil == node {
		return -fuse.ENOENT
	}

	fill(".", &node.stat, 0)
	fill("..", nil, 0)
	// for name, chld := range node.chld {
	// 	if !fill(name, &chld.stat, 0) {
	// 		break
	// 	}
	// }
	return 0
}

func (b *BeeFs) Releasedir(path string, fh uint64) (errc int) {
	defer trace(time.Now(), &errc, path, fh)
	defer b.synchronize()()

	return b.closeNode(fh)
}

func (b *BeeFs) Setxattr(path string, name string, value []byte, flags int) (errc int) {
	defer trace(time.Now(), &errc, path, name, flags)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	if "com.apple.ResourceFork" == name {
		return -fuse.ENOTSUP
	}
	if fuse.XATTR_CREATE == flags {
		if _, ok := node.xatr[name]; ok {
			return -fuse.EEXIST
		}
	} else if fuse.XATTR_REPLACE == flags {
		if _, ok := node.xatr[name]; !ok {
			return -fuse.ENOATTR
		}
	}
	xatr := make([]byte, len(value))
	copy(xatr, value)
	if nil == node.xatr {
		node.xatr = map[string][]byte{}
	}
	node.xatr[name] = xatr
	return 0
}

func (b *BeeFs) Getxattr(path string, name string) (errc int, xatr []byte) {
	defer trace(time.Now(), &errc, path, name)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT, nil
	}
	if "com.apple.ResourceFork" == name {
		return -fuse.ENOTSUP, nil
	}
	xatr, ok := node.xatr[name]
	if !ok {
		return -fuse.ENOATTR, nil
	}
	return 0, xatr
}

func (b *BeeFs) Removexattr(path string, name string) (errc int) {
	defer trace(time.Now(), &errc, path, name)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	if "com.apple.ResourceFork" == name {
		return -fuse.ENOTSUP
	}
	if _, ok := node.xatr[name]; !ok {
		return -fuse.ENOATTR
	}
	delete(node.xatr, name)
	return 0
}

func (b *BeeFs) Listxattr(path string, fill func(name string) bool) (errc int) {
	defer trace(time.Now(), &errc, path)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	for name := range node.xatr {
		if !fill(name) {
			return -fuse.ERANGE
		}
	}
	return 0
}

func (b *BeeFs) Chflags(path string, flags uint32) (errc int) {
	defer trace(time.Now(), &errc, path, flags)
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	node.stat.Flags = flags
	node.stat.Ctim = fuse.Now()
	return 0
}

func (b *BeeFs) Setcrtime(path string, tmsp fuse.Timespec) (errc int) {
	defer trace(time.Now(), &errc, path, tmsp.Time().String())
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	node.stat.Birthtim = tmsp
	node.stat.Ctim = fuse.Now()
	return 0
}

func (b *BeeFs) Setchgtime(path string, tmsp fuse.Timespec) (errc int) {
	defer trace(time.Now(), &errc, path, tmsp.Time().String())
	defer b.synchronize()()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	node.stat.Ctim = tmsp
	return 0
}

func (b *BeeFs) commitNodeFn(f *fsNode, mtdtRef, dataRef swarm.Address) func() error {
	return func() error {
		mtdtRdr, mtdtLen, err := f.metadata()
		if err != nil {
			return fmt.Errorf("failed getting metadata %w", err)
		}
		sp := splitter.NewSimpleSplitter(b.store, storage.ModePutUpload)
		addr, err := sp.Split(context.Background(), mtdtRdr, mtdtLen, b.encrypt)
		if err != nil {
			return fmt.Errorf("failed splitter %w", err)
		}
		if !addr.Equal(mtdtRef) {
			err = b.pb.Put(context.Background(), b.metadataKey(f.id), time.Now().UnixNano(), addr)
			if err != nil {
				return fmt.Errorf("failed publishing metadata %w", err)
			}
		}
		dataAddr, err := f.data.Close()
		if err != nil {
			return fmt.Errorf("failed closing file %w", err)
		}
		if !dataAddr.Equal(dataRef) {
			err = b.pb.Put(context.Background(), b.dataKey(f.id), time.Now().UnixNano(), addr)
			if err != nil {
				return fmt.Errorf("failed publishing data %w", err)
			}
		}
		return nil
	}
}

func (b *BeeFs) newNode(id string, dev uint64, ino uint64, mode uint32, uid uint32, gid uint32) *fsNode {
	tmsp := fuse.Now()
	f := &fsNode{
		id: id,
		stat: fuse.Stat_t{
			Dev:      dev,
			Ino:      ino,
			Mode:     mode,
			Nlink:    1,
			Uid:      uid,
			Gid:      gid,
			Atim:     tmsp,
			Mtim:     tmsp,
			Ctim:     tmsp,
			Birthtim: tmsp,
			Flags:    0,
		},
		xatr:    nil,
		opencnt: 0,
	}
	if fuse.S_IFREG == f.stat.Mode&fuse.S_IFREG {
		f.data = bf.New(swarm.ZeroAddress, b.store, b.encrypt)
	}
	f.commit = b.commitNodeFn(f, swarm.ZeroAddress, swarm.ZeroAddress)
	return f
}

func (b *BeeFs) metadataKey(id string) string {
	return fmt.Sprintf("%s/%s/mtdt", b.id, id)
}

func (b *BeeFs) dataKey(id string) string {
	return fmt.Sprintf("%s/%s/data", b.id, id)
}

func (b *BeeFs) lookupNode(path string) (node *fsNode) {
	ctx := context.Background()
	latest := time.Now().UnixNano()

	ref, err := b.lk.Get(ctx, b.metadataKey(path), latest)
	if err != nil {
		return nil
	}

	reader, _, err := joiner.New(context.Background(), b.store, ref)
	if err != nil {
		return nil
	}

	md, err := fromMetadata(reader)
	if err != nil {
		return nil
	}

	node = &fsNode{id: path, stat: md.Stat, xatr: md.Xatr, children: md.Children}

	dataRef := swarm.ZeroAddress
	if !node.isDir() {
		dataRef, _ = b.lk.Get(ctx, b.dataKey(path), latest)
		node.data = bf.New(dataRef, b.store, b.encrypt)
	}

	node.commit = b.commitNodeFn(node, ref, dataRef)

	return
}

func (b *BeeFs) makeNode(path string, mode uint32, dev uint64, data []byte) int {
	prnt := b.lookupNode(filepath.Dir(path))
	if nil == prnt {
		return -fuse.ENOENT
	}
	defer prnt.Close()

	node := b.lookupNode(path)
	if nil != node {
		return -fuse.EEXIST
	}
	b.ino++
	uid, gid, _ := fuse.Getcontext()
	node = b.newNode(path, dev, b.ino, mode, uid, gid)
	if nil != data {
		n, err := node.data.Write(data)
		if err != nil {
			return -fuse.EIO
		}
		node.stat.Size = int64(n)
	}

	if err := node.Close(); err != nil {
		return -fuse.EIO
	}

	prnt.children = append(prnt.children, filepath.Base(path))
	prnt.stat.Ctim = node.stat.Ctim
	prnt.stat.Mtim = node.stat.Ctim
	return 0
}

func (b *BeeFs) removeNode(path string, dir bool) int {
	prnt := b.lookupNode(filepath.Dir(path))
	if nil == prnt {
		return -fuse.ENOENT
	}
	defer prnt.Close()

	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT
	}
	defer node.Close()

	if !dir && fuse.S_IFDIR == node.stat.Mode&fuse.S_IFMT {
		return -fuse.EISDIR
	}
	if dir && fuse.S_IFDIR != node.stat.Mode&fuse.S_IFMT {
		return -fuse.ENOTDIR
	}

	if 0 < len(node.children) {
		return -fuse.ENOTEMPTY
	}
	node.stat.Nlink--
	for idx, chld := range prnt.children {
		if chld == filepath.Base(path) {
			prnt.children = append(prnt.children[:idx], prnt.children[idx+1:]...)
			break
		}
	}
	tmsp := fuse.Now()
	node.stat.Ctim = tmsp
	prnt.stat.Ctim = tmsp
	prnt.stat.Mtim = tmsp
	return 0
}

func (b *BeeFs) openNode(path string, dir bool) (int, uint64) {
	node := b.lookupNode(path)
	if nil == node {
		return -fuse.ENOENT, ^uint64(0)
	}
	if !dir && fuse.S_IFDIR == node.stat.Mode&fuse.S_IFMT {
		return -fuse.EISDIR, ^uint64(0)
	}
	if dir && fuse.S_IFDIR != node.stat.Mode&fuse.S_IFMT {
		return -fuse.ENOTDIR, ^uint64(0)
	}
	node.opencnt++
	if 1 == node.opencnt {
		b.openmap[node.stat.Ino] = node
	}
	return 0, node.stat.Ino
}

func (b *BeeFs) closeNode(fh uint64) int {
	node := b.openmap[fh]
	node.opencnt--
	if 0 == node.opencnt {
		err := node.Close()
		if err != nil {
			return -fuse.EIO
		}
		delete(b.openmap, node.stat.Ino)
	}
	return 0
}

func (b *BeeFs) getNode(path string, fh uint64) *fsNode {
	node, found := b.openmap[fh]
	if found {
		return node
	}
	nd := b.lookupNode(path)
	if nd != nil && ^uint64(0) != fh {
		b.openmap[fh] = nd
	}
	return nd
}

func (b *BeeFs) synchronize() func() {
	b.lock.Lock()
	return func() {
		b.lock.Unlock()
	}
}

var _ fuse.FileSystemChflags = (*BeeFs)(nil)
var _ fuse.FileSystemSetcrtime = (*BeeFs)(nil)
var _ fuse.FileSystemSetchgtime = (*BeeFs)(nil)
