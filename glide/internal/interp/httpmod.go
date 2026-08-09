package interp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// http host shim (M2), shaped by DESIGN.md's HTTP specifics:
// handlers RETURN values (fn(Request) -> Response, or Result of
// one); the router is Go-1.22 ServeMux level (methods + wildcards —
// literally that mux); a green thread per request; serve and get are
// blocking operations and therefore cancellation points, so a scope
// dying shuts the server down and aborts in-flight client calls —
// no ctx parameter anywhere.

type (
	RouterV struct{ routes []route }
	route   struct {
		method, pattern string
		handler         Value
	}
	RequestV struct {
		r    *http.Request
		body string
	}
	ResponseV struct {
		status      int
		contentType string
		body        string
	}
)

func (in *Interp) httpCall(name string, args []Value, line int) Value {
	switch name {
	case "router":
		if len(args) != 0 {
			panic(rtErr{line, "http.router takes no arguments"})
		}
		return &RouterV{}
	case "serve":
		if len(args) != 2 {
			panic(rtErr{line, "http.serve takes (addr, router)"})
		}
		addr, ok := args[0].(StrV)
		rt, ok2 := args[1].(*RouterV)
		if !ok || !ok2 {
			panic(rtErr{line, "http.serve takes (addr: String, router: Router)"})
		}
		return in.httpServe(string(addr), rt, line)
	case "get":
		url, ok := one("http.get", args, line).(StrV)
		if !ok {
			panic(rtErr{line, "http.get takes a URL String"})
		}
		return in.httpDo(http.MethodGet, string(url), "")
	case "post":
		if len(args) != 2 {
			panic(rtErr{line, "http.post takes (url, body) — the body is sent as JSON"})
		}
		url, ok := args[0].(StrV)
		body, ok2 := args[1].(StrV)
		if !ok || !ok2 {
			panic(rtErr{line, "http.post takes (url: String, body: String)"})
		}
		return in.httpDo(http.MethodPost, string(url), string(body))
	// Response constructors: tiny, closed set — enough to dogfood.
	case "text":
		s, ok := one("http.text", args, line).(StrV)
		if !ok {
			panic(rtErr{line, "http.text takes a String"})
		}
		return &ResponseV{status: 200, contentType: "text/plain; charset=utf-8", body: string(s)}
	case "json":
		v := one("http.json", args, line)
		var b strings.Builder
		encodeJSON(&b, v, line)
		return &ResponseV{status: 200, contentType: "application/json", body: b.String()}
	case "created":
		if len(args) != 0 {
			panic(rtErr{line, "http.created takes no arguments"})
		}
		return &ResponseV{status: 201, contentType: "text/plain; charset=utf-8", body: "created"}
	case "bad_request":
		s, ok := one("http.bad_request", args, line).(StrV)
		if !ok {
			panic(rtErr{line, "http.bad_request takes a message String"})
		}
		return &ResponseV{status: 400, contentType: "text/plain; charset=utf-8", body: string(s)}
	case "not_found":
		if len(args) != 0 {
			panic(rtErr{line, "http.not_found takes no arguments"})
		}
		return &ResponseV{status: 404, contentType: "text/plain; charset=utf-8", body: "not found"}
	}
	panic(rtErr{line, fmt.Sprintf("module http has no function %q", name)})
}

// httpServe blocks until the listener fails (Err) or the enclosing
// scope cancels (unwind, after a graceful shutdown). Each request
// runs the Glide handler on its own goroutine under the GIL, with
// the serving task's cancellation context.
func (in *Interp) httpServe(addr string, router *RouterV, line int) Value {
	cancel := in.cur.cancel
	deadline := in.cur.deadline
	mux := http.NewServeMux()
	for _, rt := range router.routes {
		handler := rt.handler
		mux.HandleFunc(rt.method+" "+rt.pattern, func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			if err != nil {
				http.Error(w, "request body too large or unreadable", http.StatusBadRequest)
				return
			}
			req := &RequestV{r: r, body: string(bodyBytes)}
			var res Value
			var handlerErr string
			in.gil.Lock()
			in.cur = &taskCtx{cancel: cancel, deadline: deadline}
			func() {
				defer func() {
					switch p := recover().(type) {
					case nil, cancelUnwind:
					case rtErr:
						handlerErr = fmt.Sprintf("line %d: %s", p.line, p.msg)
					default:
						handlerErr = fmt.Sprintf("%v", p)
					}
				}()
				res = in.callValue(handler, []Value{req}, line)
			}()
			in.gil.Unlock()
			if handlerErr != "" {
				fmt.Fprintf(in.Stderr, "http: handler panic: %s\n", handlerErr)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			writeGlideResponse(w, res)
		})
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return &ResultV{Ok: false, V: &ErrV{Msg: "listen " + addr + ": " + err.Error()}}
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	var srvErr error
	cancelled := false
	in.unblock(func() {
		select {
		case srvErr = <-served:
		case <-cancel:
			cancelled = true
			ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			_ = srv.Shutdown(ctx)
			stop()
		}
	})
	if cancelled {
		panic(cancelUnwind{})
	}
	return &ResultV{Ok: false, V: &ErrV{Msg: "server stopped: " + srvErr.Error()}}
}

// writeGlideResponse maps the handler's return: a Response writes
// itself; Ok(Response) unwraps; Err(e) is the one default
// error-to-status mapping (500 + rendered error). Anything else is
// a handler bug.
func writeGlideResponse(w http.ResponseWriter, res Value) {
	if r, isRes := res.(*ResultV); isRes {
		if !r.Ok {
			http.Error(w, display(r.V), http.StatusInternalServerError)
			return
		}
		res = r.V
	}
	resp, ok := res.(*ResponseV)
	if !ok {
		http.Error(w, fmt.Sprintf("handler returned %s, not a Response", typeName(res)), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", resp.contentType)
	w.WriteHeader(resp.status)
	_, _ = io.WriteString(w, resp.body)
}

// httpDo: production defaults (a timeout exists out of the box);
// the enclosing scope's cancellation and deadline abort the request
// via the bridged context.
func (in *Interp) httpDo(method, url, body string) Value {
	ctx, release := in.hostCtx()
	var resp *ResponseV
	var failure string
	cancelled := false
	in.unblock(func() {
		defer release()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, rd)
		if err != nil {
			failure = err.Error()
			return
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		client := &http.Client{Timeout: 30 * time.Second}
		r, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				cancelled = true
				return
			}
			failure = err.Error()
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			failure = err.Error()
			return
		}
		resp = &ResponseV{status: r.StatusCode, contentType: r.Header.Get("Content-Type"), body: string(body)}
	})
	if cancelled {
		panic(cancelUnwind{})
	}
	if failure != "" {
		return &ResultV{Ok: false, V: &ErrV{Msg: failure}}
	}
	return &ResultV{Ok: true, V: resp}
}
