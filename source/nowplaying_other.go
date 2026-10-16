//go:build !windows

package main

import "errors"

func nowPlayingSMTC() (NowPlaying, error)   { return NowPlaying{}, errors.New("only available on Windows") }
func nowPlayingWindow() (NowPlaying, error) { return NowPlaying{}, errors.New("only available on Windows") }
