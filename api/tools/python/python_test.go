package python

import (
	"encoding/json"
	"testing"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetLevel(log.WarnLevel)
}

func eval(t *testing.T, c *Python, code, expect string) {
	args, _ := json.Marshal(map[string]string{"code": code})
	_, resp, err := c.Call(string(args))
	t.Logf("\n%s", resp)
	if err != nil {
		t.Error(err)
	}
	if resp != expect {
		t.Errorf("expected %q - got %q", expect, resp)
	}
}

func TestHello(t *testing.T) {
	c := New()
	defer c.Stop()
	eval(t, c, `print("Hello world!")`, "Hello world!\n")
}

func TestExpr(t *testing.T) {
	c := New()
	defer c.Stop()
	eval(t, c, "x = 2\ny = 21\nx*y", "42\n")
}

func TestSympy(t *testing.T) {
	c := New()
	defer c.Stop()
	eval(t, c, "import sympy as sp\nprint(sp.primepi(20))", "8\n")
}

func TestError(t *testing.T) {
	c := New()
	defer c.Stop()
	eval(t, c, `print("foo"); print(bar)`, "foo\nNameError: name 'bar' is not defined\n")
}

func TestNumpy(t *testing.T) {
	c := New()
	defer c.Stop()
	src := `
import numpy as np

a = np.array([1, 2, 3])
b = np.array([4, 5, 6])
res = np.dot(a, b)

print(f"{a} . {b} = {res}")
`
	eval(t, c, src, "[1 2 3] . [4 5 6] = 32\n")
}

func TestFilesystem(t *testing.T) {
	c := New()
	defer c.Stop()
	src := `
with open("/etc/hosts") as f:
	print(f.readline(), end="")
with open("foo.txt", "w") as f:
	f.write("test write")
`
	eval(t, c, src, "127.0.0.1	localhost\n")

	src = `
with open("foo.txt") as f:
	print(f.read())
`
	eval(t, c, src, "test write\n")
}

func TestNetwork(t *testing.T) {
	c := New()
	defer c.Stop()
	src := `
import http.client
conn = http.client.HTTPConnection("www.example.com")
conn.request("GET", "/")
r1 = conn.getresponse()
print(r1.status, r1.reason)
`
	msg := "socket.gaierror: [Errno -3] Temporary failure in name resolution"
	eval(t, c, src, msg+"\n")
}

func TestTruncate(t *testing.T) {
	cfg := DefaultConfig
	cfg.OutputBytes = 50
	c := New(cfg)
	defer c.Stop()

	src := `
for n in range(1000):
    print(f"n = {n}")
`
	expect := `n = 0
n = 1
n = 2
n = 3
n = 4
n = 5
n = 6
n = 7
n 
=== output truncated ===
`
	eval(t, c, src, expect)
}

func TestTimeout(t *testing.T) {
	cfg := DefaultConfig
	cfg.TimeSeconds = 5
	c := New(cfg)
	defer c.Stop()

	src := `
import time

time.sleep(0.5)
for n in range(10):
    time.sleep(1)
    print(f"n = {n}")
`
	expect := `n = 0
n = 1
n = 2
n = 3

Error: timed out - killed
`
	eval(t, c, src, expect)
}

func TestMemlimit(t *testing.T) {
	cfg := DefaultConfig
	cfg.MemoryBytes = 10 * 1024 * 1024
	c := New(cfg)
	defer c.Stop()

	src := `
print("start")

import numpy as np
a = np.arange(10_000_000)
print(a.shape)
`
	eval(t, c, src, "start\n\nError: execution failed\n")
}
