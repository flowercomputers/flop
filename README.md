# flop

flop is the command line for [Flower Panel](https://panel.flowercomputer.com): you can use it to manage bog deployments on our infrastructure.

## Install

```
curl -fsSL https://flowercomputer.com/install.sh | sh
```

macOS or Linux, arm64 or amd64. It installs the latest release at `~/.local/bin/flop` (`FLOP_INSTALL_DIR` for another place, `FLOP_VERSION=0.3.0` for another version), and running it again updates flop.

A release's files are at `https://dl.flowercomputer.com/flop/v<version>/flop_<version>_<os>_<arch>.tar.gz`, with `checksums.txt` beside them; `https://dl.flowercomputer.com/flop/latest` is the newest version.

## Use

```
flop auth login        # sign in: you approve this device in the browser
flop init notes        # start a project: a small working bog in ./notes
flop bog deploy        # build the project in the current git repository and run it
flop --help            # every command
```

Every command answers in JSON with `--json`. `flop docs` prints the guide for scripts and agents ([AGENTS.md](AGENTS.md)): signing in with a token, output, failures and exit statuses, common flows.

## Updates

`flop update` installs the latest release over flop, as the install script does. 

## This repository is a mirror

flop is developed in Flower Computer's own repository and published here with each release. Issues and PRs are welcome here but will be implemented in our monorepo, then released here.

Use this to verify a build was built through this repo: 
`gh attestation verify flop_<version>_<os>_<arch>.tar.gz --repo flowercomputers/flop`
