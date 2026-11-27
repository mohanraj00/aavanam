# Aavanam backend

## Read listener

The service uses TCP address `127.0.0.1:8437` by default. Use `--read-addr` to set a different TCP address.

On Unix, use `--read-socket` with an absolute path to serve the same read API on a Unix socket. This option replaces the TCP listener. If you set `--read-socket`, omit `--read-addr`. Set `--read-socket-mode` to an octal file mode. The default mode is `0600`.

```sh
aavanam-server --read-socket /run/library-catalog/read.sock --read-socket-mode 0660
curl --unix-socket /run/library-catalog/read.sock http://localhost/v1/about
```

NET-TRN-1 specifies HTTP/1.1 over TCP. NET-TRN-5 sets the default TCP address and requires a separate listener for writes. The Unix socket is a service option outside the transport defined by the binding. It uses the same HTTP paths, request bodies, limits and result envelopes. `--write-socket` can still set a separate socket for writes. Its mode is `0600`.

On Linux, the socket mode can prevent other local users from reading the store. Use `0660` to give access to the socket owner and members of its group. Set the group through the service account or the socket directory. Give permitted clients search permission on the directories in the path. Keep the socket directory and its parents under the control of the service or its administrator. Give client users no write permission on those directories. Use directory permissions to restrict access on systems that do not check socket modes on connect.

Do not put an inheritable ACL on the socket directory. The service checks only the mode bits of the socket file. On macOS, a new socket file inherits the entries of an inheritable ACL of its directory. Such an entry can give a different local account access to a socket with mode `0600`. Only the owner of the directory or an administrator can set that ACL. On macOS, `ls -lde <directory>` shows the ACL of a directory.

On AIX and on Solaris other than illumos, the service refuses to start if a socket file already exists at the specified path. After an unclean stop, make sure that no service uses the socket. Then remove the socket file by hand before you restart the service.

On the other supported Unix systems, the service adds `.lock` to each socket path and creates a private lock file with mode `0600`. The service holds the lock from its start until its listener closes. If the lock is in use, a second start waits up to two seconds, then fails. Thus a second start on the same socket path cannot remove the socket of a service that runs. The lock file stays in place after the service stops. Do not remove the lock file while a service runs. The service replaces a socket left by an earlier run only if it gets the lock and the operating system refuses a connection. If the connection check gives a permission error, the service stops and keeps the socket.

A service of version 0.5.0 does not hold the lock while it runs. Stop that service before you start a later version on the same socket path.

## Profiles

By default, the store of the service declares only the base profile. Use `--profile` to declare a profile. The option can occur more than one time.

```sh
aavanam-server --profile aavanam/layer/v1 --profile aavanam/semantic-index/v1
```

`aavanam/layer/v1` is the layer profile of document 23. `aavanam/semantic-index/v1` is the retrieval index profile of document 24. A store that does not declare a profile refuses its kinds, its fields and its operation (LYR-PRF-1, IDX-PRF-1). The read listener does not have `semantic_search`, because NET-SCP-1 does not list it. The listener for the writes has it.
