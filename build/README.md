# build/

- `appicon.png`: application icon used by `wails build`
- `darwin/Info.plist`, `darwin/Info.dev.plist`: macOS bundle templates for
  `wails build` and `wails dev` (bundle identifier `dev.gitrais.app`)
- `git-dist/`: the bundled git, built by `make git` (not committed)
- `bin/`: build output (`Gitrais.app`, `askpass-helper`; not committed)

`make package` copies `git-dist` and `askpass-helper` into
`bin/Gitrais.app/Contents/Resources` and ad-hoc signs the bundle.
