// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

package service

import "fmt"

func companionCommandLine(exePath string, serviceArgs []string) string {
	cmd := fmt.Sprintf("%q companion", exePath)
	for i := 0; i < len(serviceArgs)-1; i++ {
		if serviceArgs[i] == "--config" {
			cmd += fmt.Sprintf(" --config %q", serviceArgs[i+1])
			break
		}
	}
	return cmd
}
