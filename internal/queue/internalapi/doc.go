// Package internalapi is the app's second listener (INTERNAL_ADDR, specs.html § 2.1): the
// worker routes only, behind the shared worker secret. It sits under internal/queue rather
// than internal/api because Go refuses imports of an "internal" directory nested in
// internal/api from the entrypoint.
package internalapi
