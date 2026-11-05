//go:build !darwin

package main

func (a *App) installPlatformUpdate() (bool, error) { return false, nil }
