//go:build !windows && !darwin

package main

func serviceAction(_ string) error { return nil }
