// Command aavanam-server is the backend service: one writer store in PostgreSQL with two listeners.
//
// The read listener has the network read binding `aavanam-read/v1` (document 22): the operations that do
// not write content. It uses the loopback address unless --read-addr gives a different one (NET-TRN-5).
//
// The write listener has each operation of the operation table, with the same paths. It is not a part of
// the binding. It is a Unix socket that only the user of the service can open, and the service has it only
// with --write-socket.
//
// Access control is not in the scope of the specification. A deployment puts its access control in front
// of the read listener, and it controls who can open the socket.
//
// The DSN comes from the environment variable AAVANAM_PG_DSN, not from an argument, because the arguments
// of a process are visible to other users. The gates, the sections and the policies are store
// configuration (PUB-GAT-1, PUB-SEC-1, PUB-RCP-1). The service declares them at each start, before the
// first call. A validator of a policy is an HTTP address of the host application (api.HTTPValidator). The
// service never loads or runs a stored artifact.
//
// A read context has a life (--context-life). After its end, a call in the context gets `context_expired`,
// and the service removes the context. Thus the calls of `open_context` do not make the database larger
// with no limit in time. A limit for each caller is access control, which the deployment owns.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mohanraj00/aavanam/backend/api"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/text"
	"github.com/mohanraj00/aavanam/backend/core/transfer"
	"github.com/mohanraj00/aavanam/backend/core/write"
	"github.com/mohanraj00/aavanam/backend/pg"
)

// pairs is a flag that can occur more than one time. Each value is `name` or `name=value`.
type pairs struct {
	valued bool
	list   [][2]string
}

func (p *pairs) String() string { return fmt.Sprint(p.list) }

func (p *pairs) Set(text string) error {
	if !p.valued {
		p.list = append(p.list, [2]string{text, ""})
		return nil
	}
	// The first `=` ends the name, because the URL of a validator can have one. Thus these flags cannot
	// give a name that has `=`.
	name, value, found := strings.Cut(text, "=")
	if !found || name == "" || value == "" {
		return errors.New("the value has the form name=value")
	}
	p.list = append(p.list, [2]string{name, value})
	return nil
}

// options is the configuration of the service.
type options struct {
	readAddr          string
	writeSocket       string
	maxBody, writeMax int64
	deadline          time.Duration
	validatorDeadline time.Duration
	contextLife       time.Duration
	gates             pairs
	sections          pairs
	policies          pairs
}

func parse(args []string, output io.Writer) (*options, error) {
	o := &options{sections: pairs{valued: true}, policies: pairs{valued: true}}
	flags := flag.NewFlagSet("aavanam-server", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&o.readAddr, "read-addr", "127.0.0.1:8437", "the address of the read listener (NET-TRN-5)")
	flags.StringVar(&o.writeSocket, "write-socket", "", "the path of the Unix socket of the write listener (default: no write listener)")
	flags.Int64Var(&o.maxBody, "max-body-bytes", api.DefaultMaxBodyBytes, "the limit of a request body of the read listener (NET-LIM-1)")
	flags.Int64Var(&o.writeMax, "write-max-body-bytes", 256<<20, "the limit of a request body of the write listener")
	flags.DurationVar(&o.deadline, "deadline", api.DefaultDeadline, "the deadline of one call (NET-LIM-2)")
	flags.DurationVar(&o.validatorDeadline, "validator-deadline", api.DefaultValidatorDeadline, "the deadline of a validator (PUB-RCP-4)")
	flags.DurationVar(&o.contextLife, "context-life", 24*time.Hour, "the life of a read context, or 0 for no end")
	flags.Var(&o.gates, "gate", "a gated collection (PUB-GAT-1). The flag can occur more than one time")
	flags.Var(&o.sections, "section", "collection=serving or collection=evidence (PUB-SEC-1). The flag can occur more than one time")
	flags.Var(&o.policies, "policy", "name=URL: an acceptance policy and the HTTP address of its validator (PUB-RCP-1). The flag can occur more than one time")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() > 0 {
		return nil, fmt.Errorf("an argument that is not a flag: %q", flags.Arg(0))
	}
	if o.maxBody <= 0 || o.writeMax <= 0 || o.deadline <= 0 || o.validatorDeadline <= 0 {
		return nil, errors.New("a limit and a deadline are more than zero")
	}
	if o.contextLife < 0 {
		return nil, errors.New("the life of a read context is zero or more")
	}
	return o, nil
}

// service is the running listeners of one store.
type service struct {
	servers   []*http.Server
	listeners []net.Listener
	ReadURL   string
	failed    chan error
	stopped   chan struct{}
}

// expirer is a storage whose read contexts have a life (pg.Store).
type expirer interface {
	SetContextLife(life time.Duration)
	ExpireContexts(ctx context.Context, grace time.Duration) (int64, error)
}

var _ expirer = (*pg.Store)(nil)

// expire removes the read contexts after their life, again and again, until the service stops. The grace
// time is longer than a call, so a call in progress keeps the content list of its context. A removal that
// fails has no effect on a call: the next run does it again.
func (s *service) expire(storage expirer, every, grace time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopped:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			_, _ = storage.ExpireContexts(ctx, grace)
			cancel()
		}
	}
}

// start declares the configuration on the store and opens the listeners.
func start(ctx context.Context, o *options, storage ops.Storage) (*service, error) {
	registry := ops.NewRegistry()
	read.Register(registry)
	text.Register(registry)
	transfer.Register(registry)
	write.Register(registry)
	store := ops.New(ops.Writer, storage, registry)
	for _, policy := range o.policies.list {
		validate, err := api.HTTPValidator(policy[0], policy[1], o.validatorDeadline)
		if err != nil {
			return nil, fmt.Errorf("the policy %q: %w", policy[0], err)
		}
		store.Accept(policy[0], validate)
	}
	for _, gate := range o.gates.list {
		if err := store.Gate(ctx, gate[0]); err != nil {
			return nil, fmt.Errorf("the gate %q: %w", gate[0], err)
		}
	}
	for _, section := range o.sections.list {
		if err := store.Section(ctx, section[0], section[1]); err != nil {
			return nil, fmt.Errorf("the section of %q: %w", section[0], err)
		}
	}
	current := func() *ops.Store { return store }
	s := &service{failed: make(chan error, 2), stopped: make(chan struct{})}
	serve := func(listener net.Listener, handler http.Handler) {
		server := api.Server(handler, o.deadline)
		s.servers, s.listeners = append(s.servers, server), append(s.listeners, listener)
		go func() {
			if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
				s.failed <- err
			}
		}()
	}
	// The two listeners open before one of them runs: the file mode of the socket needs the mask of the
	// process. The TCP address comes first, so a start that cannot have it does not touch the socket path.
	readListener, err := net.Listen("tcp", o.readAddr)
	if err != nil {
		return nil, fmt.Errorf("the read listener: %w", err)
	}
	var writeListener net.Listener
	if o.writeSocket != "" {
		if writeListener, err = listenSocket(o.writeSocket); err != nil {
			_ = readListener.Close()
			return nil, fmt.Errorf("the write listener: %w", err)
		}
	}
	s.ReadURL = "http://" + readListener.Addr().String()
	serve(readListener, api.Read(api.Config{Store: current, MaxBodyBytes: o.maxBody, Deadline: o.deadline}))
	if writeListener != nil {
		serve(writeListener, api.Write(api.Config{Store: current, MaxBodyBytes: o.writeMax, Deadline: o.deadline}))
	}
	if contexts, has := storage.(expirer); has && o.contextLife > 0 {
		contexts.SetContextLife(o.contextLife)
		every := o.contextLife / 4
		if every > 5*time.Minute {
			every = 5 * time.Minute
		}
		go s.expire(contexts, every, 2*o.deadline+time.Minute)
	}
	return s, nil
}

// stop closes the listeners. A call in progress has 10 seconds to end.
func (s *service) stop() {
	close(s.stopped)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, server := range s.servers {
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
		}
	}
}

// socketListener opens the socket of a path. It removes a socket that an earlier run left at the path. It
// does not remove a file of a different type, and it does not remove the socket of a service that runs.
func socketListener(path string, listen func(path string) (net.Listener, error)) (net.Listener, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("the path has a file that is not a socket")
		}
		// A socket that accepts a connection has a service. Only a socket with no listener is removed.
		if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
			_ = conn.Close()
			return nil, errors.New("a service that runs has the socket")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return listen(path)
}

func run(args []string) error {
	o, err := parse(args, os.Stderr)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	opening, done := context.WithTimeout(ctx, 30*time.Second)
	database, err := pg.Open(opening, os.Getenv("AAVANAM_PG_DSN"))
	done()
	if err != nil {
		return err
	}
	defer database.Close()
	s, err := start(ctx, o, database)
	if err != nil {
		return err
	}
	defer s.stop()
	fmt.Fprintln(os.Stderr, "aavanam-server: the read listener is at", s.ReadURL)
	select {
	case <-ctx.Done():
		return nil
	case err := <-s.failed:
		return err
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "aavanam-server:", err)
		os.Exit(1)
	}
}
