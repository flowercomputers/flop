package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/flowercomputers/flop/api"
)

// built is a build's outcome: the image it pushed
type built struct {
	ID      string `json:"id,omitempty"`
	Package string `json:"package"`
	Version string `json:"version"` // the commit, -dirty for uncommitted changes
	Image   string `json:"image"`
}

func (b built) human(w io.Writer) {
	fmt.Fprintf(w, "Built %s (%s): %s\n", b.Package, b.Version, b.Image)
}

// buildImage packages the current git repo, builds it remotely, and returns
// what it built. The build's output goes to stderr, as it comes.
func buildImage(ctx context.Context, o *ui, client *Client, packageFlag string, fresh bool, arch string) (built, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return built{}, err
	}
	root, err := gitRoot(cwd)
	if err != nil {
		return built{}, usage(err.Error(), "run flop inside the git repository of a Rust project")
	}

	pkg, err := localPackage(packageFlag)
	if err != nil {
		return built{}, err
	}

	sha, dirty := gitMeta(root)
	version := sha
	if dirty {
		version += "-dirty"
	}
	if version == "" {
		version = "no-commits"
	}
	o.say("Packaging %s (%s)", pkg, version)

	tarball, err := sourceTarball(root)
	if err != nil {
		return built{}, err
	}
	if len(tarball) > api.MaxTarballBytes {
		return built{}, usage(fmt.Sprintf("the source tarball is %.1f MB, over the %d MB limit", mb(len(tarball)), api.MaxTarballBytes/(1<<20)),
			"keep build output and large files out of git (.gitignore)")
	}
	o.say("Uploading %.1f MB...", mb(len(tarball)))

	stream, err := client.StartBuild(ctx, StartBuildRequest{
		Package: pkg,
		GitSHA:  sha,
		Dirty:   dirty,
		Fresh:   fresh,
		Tarball: tarball,
		Arch:    arch,
	})
	if err != nil {
		return built{}, fmt.Errorf("build: %w", err)
	}
	defer stream.Close()
	if stream.BuildID != "" {
		o.say("Build started: %s", stream.BuildID)
	}

	done, err := stream.Wait(func(ev api.BuildEvent) {
		io.WriteString(o.err, ev.Out+ev.Err)
	})
	if err != nil {
		return built{}, fmt.Errorf("build: %w", err)
	}
	if done.ExitCode != 0 {
		return built{}, &cliError{Code: "build_failed", Message: fmt.Sprintf("the build of %s failed (exit code %d)", pkg, done.ExitCode),
			Hint: "its output is above, on stderr; fix it and deploy again"}
	}
	o.say("Build succeeded: %s", done.Image)
	return built{ID: stream.BuildID, Package: pkg, Version: version, Image: done.Image}, nil
}

func mb(n int) float64 {
	return float64(n) / (1 << 20)
}

// cargoPackageName extracts the [package] name from a Cargo.toml. Minimal
// parser for the conventional `name = "x"` form; --package is the escape
// hatch for anything fancier (and for cargo workspaces with no root package).
// localPackage is the package a build here makes: packageFlag, or the git
// root's Cargo.toml's
func localPackage(packageFlag string) (string, error) {
	if packageFlag != "" {
		return packageFlag, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := gitRoot(cwd)
	if err != nil {
		return "", usage(err.Error(), "run flop inside the git repository of a Rust project")
	}
	pkg, err := cargoPackageName(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		return "", usage(err.Error(), "pass --package <name>")
	}
	return pkg, nil
}

func cargoPackageName(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no Cargo.toml at the repository root; is this a Rust project? (%w)", err)
	}
	inPackage := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inPackage = line == "[package]"
			continue
		}
		if !inPackage {
			continue
		}
		rest, ok := strings.CutPrefix(line, "name")
		if !ok {
			continue
		}
		rest, ok = strings.CutPrefix(strings.TrimSpace(rest), "=")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(strings.SplitN(rest, "#", 2)[0])
		if name := strings.Trim(rest, `"'`); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("no [package] name in %s; pass --package", path)
}
