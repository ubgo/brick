// Package runtime holds brick's browser runtime, embedded so the engine
// serves it with no build step:
//
//   - runtime.js mounts component client code;
//   - nav.js turns internal links and forms into in-place
//     page updates, when Options.Navigation is on.
package runtime

import _ "embed"

// JS is the mount runtime. The engine serves it content-hashed and with SRI
// from Engine.Assets(), first in every {{ assets }} block that has client
// code or navigation.
//
//go:embed runtime.js
var JS []byte

// Nav is the navigation runtime. It needs JS loaded first.
//
//go:embed nav.js
var Nav []byte
