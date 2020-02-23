package main

import (
	"bytes"

	"github.com/sirkon/go-format"
)

// Collector line by line collector
type Collector struct {
	buf bytes.Buffer
}

// Line puts format expression
func (r *Collector) Line(line string, p ...interface{}) {
	r.buf.WriteString(format.Formatp(line, p...))
	r.buf.WriteByte('\n')
}

// Rawl puts raw string
func (r *Collector) Rawl(line string) {
	r.buf.WriteString(line)
	r.buf.WriteByte('\n')
}

// Newl puts new line
func (r *Collector) Newl() {
	r.buf.WriteByte('\n')
}

// Bytes returns collected data in []byte
func (r *Collector) Bytes() []byte {
	return r.buf.Bytes()
}

// String returns collected data in string
func (r *Collector) String() string {
	return r.buf.String()
}
