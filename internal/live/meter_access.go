package live

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var accessInbound = regexp.MustCompile(`\[in-([a-f0-9]{32})(?:\s|\])`)
var accessUser = regexp.MustCompile(`email:\s*([a-f0-9]{32})(?:\s|$)`)

type accessWriter struct {
	mu       sync.Mutex
	pending  []byte
	sequence uint64
	meter    *kernelMeter
	discard  bool
}

func (w *accessWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	size := len(p)
	for len(p) > 0 {
		n := bytes.IndexByte(p, '\n')
		if n < 0 {
			if len(w.pending)+len(p) <= 8192 && !w.discard {
				w.pending = append(w.pending, p...)
			} else {
				w.pending = nil
				w.discard = true
			}
			break
		}
		if !w.discard && len(w.pending)+n <= 8192 {
			w.pending = append(w.pending, p[:n]...)
			w.line(string(w.pending))
		}
		w.pending = nil
		w.discard = false
		p = p[n+1:]
	}
	return size, nil
}
func (w *accessWriter) line(line string) {
	i := strings.Index(line, " accepted ")
	if i < 0 {
		return
	}
	in := accessInbound.FindStringSubmatch(line)
	if len(in) != 2 {
		return
	}
	prefix := strings.Fields(line[:i])
	if len(prefix) == 0 {
		return
	}
	source := prefix[len(prefix)-1]
	tail := strings.Fields(line[i+len(" accepted "):])
	if len(tail) == 0 {
		return
	}
	parts := strings.SplitN(tail[0], ":", 2)
	if len(parts) != 2 {
		return
	}
	user := ""
	if u := accessUser.FindStringSubmatch(line); len(u) == 2 {
		user = u[1]
	}
	w.sequence++
	w.meter.store.observe(fmt.Sprint(w.meter.secret, w.sequence), user, in[1], source, parts[1], "", parts[0])
}
