//go:build darwin

/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: SysRAMDarwin
 *
 * Provides systemTotalRAM() for macOS using hw.physmem via the syscall package.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 04.05.2026
 *
 */

package main

import "syscall"

// systemTotalRAM returns the physical RAM in bytes.
// hw.physmem is a uint32 that saturates at UINT32_MAX (~4 GiB) on larger machines,
// so the result is a lower bound — still sufficient for the cache threshold check.
func systemTotalRAM() uint64 {
	n, err := syscall.SysctlUint32("hw.physmem")
	if err != nil {
		return 0
	}
	return uint64(n)
}
