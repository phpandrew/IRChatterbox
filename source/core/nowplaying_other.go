//go:build !windows

package core

import "errors"

func nowPlayingSMTC() (NowPlaying, error) {
	return NowPlaying{}, errors.New("only available on Windows")
}
func nowPlayingWindow() (NowPlaying, error) {
	return NowPlaying{}, errors.New("only available on Windows")
}
