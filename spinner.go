package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// the spinner draws on stderr, where the badge it sits under goes
func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func runWithLineSpinner(display string, spinnerLine int, message string, fn func() error) error {
	if spinnerLine < 0 || !stderrIsTerminal() {
		return fn()
	}

	// the cursor waits on the spinner line, after the message, where a caret
	// reads naturally; it returns below the display once fn is done
	linesUp := strings.Count(display, "\n") + 1 - spinnerLine
	fmt.Fprintf(os.Stderr, "\033[%dA", linesUp)
	draw := func(i int) {
		fmt.Fprintf(os.Stderr, "\r\033[2K%s %s", spinnerFrames[i%len(spinnerFrames)], message)
	}
	draw(0)

	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for i := 1; ; i++ {
			select {
			case <-done:
				fmt.Fprintf(os.Stderr, "\r\033[2K\033[%dB\r", linesUp)
				return
			case <-ticker.C:
				draw(i)
			}
		}
	}()

	err := fn()
	close(done)
	<-stopped
	return err
}
