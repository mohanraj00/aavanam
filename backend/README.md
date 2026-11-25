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

The service replaces a socket left by an earlier run only if it can lock the directory and the operating system refuses a connection. If the check gives a permission error, the service stops and keeps the file.
