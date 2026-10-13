//go:build production

package gitexec

// productionBuild is true in `wails build` binaries (tag "production"), false
// in `wails dev` builds, which also run from a .app bundle, and in tests.
const productionBuild = true
