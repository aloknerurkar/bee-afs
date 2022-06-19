# bee-afs
Active FUSE filesystem for bee

AFS stands for Active File System, which means this filesystem implementation can be used to create dynamic FUSE mounts on Swarm which are mutable.
Every time the file is changed, it is synced with the Swarm network and updates are sent for it on the network using Feeds which can be used
to get the latest version of the file by other clients.

A postage batch can be configured per mount point.

`bee-afs` uses [billziss-gh/cgofuse](https://github.com/billziss-gh/cgofuse). This was chosen as it is supported on all the
platforms (Windows included! Phew!)

The FUSE implementation is based on the in-memory filesystem implementation inside `cgofuse`. Additionally, the implementation
stores files using `bee-file (pkg/file)` instead of in-memory. This stores the writes in-memory till the file is `closed` or `synced` manually
and written in the format used by `bee`.

NOTE: This project was built as part of a hackathon. So not everything is tested. Please use at your own risk. Any issues or PRs are welcome.

`bee-afs` is packaged as a CLI application with just 2 commands. Users can create mounts or list their existing mounts. The flags can be provided
using a config file or on the command-line.

```
NAME:
   bee-afs mount create - 

USAGE:
   bee-afs mount create [command options] [arguments...]

OPTIONS:
   --api-host value        (default: http://localhost)
   --api-port value        (default: 1633)
   --config FILE, -c FILE  Load configuration from FILE [$BEEAFS_CONFIG]
   --debug                 enable all logs (default: false)
   --encrypt               (default: false)
   --inmem                 use inmem storage for testing (default: false)
   --password value        password for swarm-key file
   --pin                   (default: false)
   --postage-batch value   
   --swarm-key value       path to swarm-key file
```

```
NAME:
   bee-afs mount list - 

USAGE:
   bee-afs mount list [command options] [arguments...]

OPTIONS:
   --api-host value        (default: http://localhost)
   --api-port value        (default: 1633)
   --config FILE, -c FILE  Load configuration from FILE [$BEEAFS_CONFIG]
   --debug                 enable all logs (default: false)
   --encrypt               (default: false)
   --inmem                 use inmem storage for testing (default: false)
   --password value        password for swarm-key file
   --pin                   (default: false)
   --postage-batch value   
   --swarm-key value       path to swarm-key file
```

## Quickstart
- Install [FUSE](http://github.com/libfuse/libfuse) for your OS.

- Install `bee-afs`\
  `bee-afs` is a go project. So you can clone it locally and build the `cmd` package.
  
- Install `bee`\
  So this is a pre-requisite to run the application. It is advisable to run bee-afs against a local bee node for better latencies.
  For testing you can use bee node in dev mode, which runs an in-mem node not connected to the network. Once the node is up, you need
  to create a dummy postage batch using the dev node.
  ```
  ./bee dev
  
  curl -X POST http://localhost:1635/stamps/{amount}/{depth}
  ```

- Create a configuration file. A basic config would look like this.
   ```
   # if key and password is not provided we generate a dummy one. This will change on each restart so no data would be retrievable as all the
   # data is tied to users private key. This could be useful for testing.
   
   # swarm-key: <PATH TO KEY FILE>
   # password: <PASSWORD>                                                                                                                                               
   api-host: "localhost"                                                                                                                                            
   api-port: 1633                                                                                                                                                   
   postage-batch: <BATCH CREATED ABOVE>                                                                              
   ```
  
- Mount a directory. This will create the fuse mount and make it active. The program will not return, in order to stop it, you can use `Ctrl-C`.
  ```
  ./bee-fs mount create <UNIQUE NAME FOR MOUNT> <PATH TO DIRECTORY ON MACHINE> --config <PATH TO CONFIG>
  ```

## Design
`bee-afs` uses the concept of feeds. To read more about feeds please refer [the book of swarm](https://www.ethswarm.org/The-Book-of-Swarm.pdf). Feeds are
useful to perform versioning of a mutable resource. We use the epoch based indexing scheme here. The epoch based indexing scheme can be used to post
version updates in the form of epochs. This allows us to get the latest updates to an item or even go back in time and construct the filesystem for that
epoch. This way, we can store the filesystem along with all the historical data in the swarm network. Data is deduped at the chunk level, so only chunks
which are unique are stored again.

When user configures a mount, he has to name it. Each item in the FS (directory/file) is represented as a feed. For eg.
```
./bee-afs mount create [command options] <MOUNT NAME> <PATH TO DIRECTORY>
```
For a directory there will be only 1 feed which is the metadata feed. For files, we will have metadata and data feed. The latest update in the feed
points to the latest state of the file/directory. They topics for the feed are created as follows:

```
MOUNT_NAME/<PATH TO DIR/FILE>/mtdt
MOUNT_NAME/<PATH TO FILE>/data
```

So with this type of naming of the topics, we don't need to maintain any overarching manifest structure for the filesystem. We can query each item
based on their full path and knowing the mount we are working on. Also users can create separate unique mounts by simply naming them uniquely.

Metadata feed will return a swarm reference to the metadata bytes which can be obtained from the network. Similarly the data feed will return the latest
swarm reference of the file bytes.

Users can mount the same filesystems from different devices as long as they use the same private key on both of them. The topic is hashed using the keys
so keys provide a different namespace as well.
