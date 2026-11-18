package main

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"

	"github.com/flowercomputers/authbadge"
	"github.com/flowercomputers/vase"
)

const appName = "Flowerpanel (Flop)"

// appVersion is a release's version, stamped at its build (-ldflags
// "-X main.appVersion=0.1.0"); "dev" in any other build
var appVersion = "dev"

func renderAuthAvatar(principalID string, loggedInAs string, color bool) (string, error) {
	avatar, err := renderAvatar(principalID, color)
	if err != nil {
		return "", err
	}

	return composeAuthAvatar(avatar, loggedInAs), nil
}

// renderPendingAuthBadge draws the user code as the same badge the browser's
// confirm page shows, so the two can be matched at a glance.
func renderPendingAuthBadge(userCode string, color bool) (string, int) {
	return composeAnnotatedAvatar(authbadge.ANSI(userCode, color), map[int]string{
		1: "This badge should match your browser.",
	}, true)
}

func renderAvatar(seed string, color bool) (string, error) {
	if seed == "" {
		return "", errors.New("seed is empty")
	}

	rng := rand.New(rand.NewSource(avatarSeed(seed)))
	model, err := vase.Generate(rng, vase.DefaultOptions())
	if err != nil {
		return "", fmt.Errorf("generate avatar: %w", err)
	}

	return vase.Render(model, vase.RenderOptions{Color: color}), nil
}

func composeAuthAvatar(avatar string, loggedInAs string) string {
	out, _ := composeAnnotatedAvatar(avatar, map[int]string{
		2: appName,
		4: "v " + spacedVersion(appVersion),
		6: "Logged in as " + loggedInAs,
	}, false)
	return out
}

func composeAnnotatedAvatar(avatar string, labels map[int]string, withSpinnerLine bool) (string, int) {
	lines := strings.Split(avatar, "\n")
	for i, line := range lines {
		if label := labels[i]; label != "" {
			lines[i] = line + "  " + label
		}
	}

	spinnerLineIndex := -1
	if withSpinnerLine {
		lines = append(lines, "")
		spinnerLineIndex = len(lines)
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n"), spinnerLineIndex
}

func spacedVersion(version string) string {
	return strings.Join(strings.Split(version, ""), " ")
}

func avatarSeed(seed string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	return int64(h.Sum64())
}
