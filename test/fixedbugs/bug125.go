// errchk $G $D/$F.go

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	OS "os"  // should require semicolon here; this is no different from other decls
	IO "io"  // ERROR "missing|syntax"
)

func main() {
}
