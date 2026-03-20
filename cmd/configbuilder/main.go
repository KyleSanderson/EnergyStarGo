// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

// Command configbuilder provides an interactive CLI tool to build and edit EnergyStarGo config files.
package main

import (
	"fmt"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/viper"
)

func main() {
	fmt.Println("EnergyStarGo Config Builder")

	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Println("No config found, creating new one.")
	}

	// Session targeting
	var sessionTarget string
	survey.AskOne(&survey.Select{
		Message: "Session Targeting:",
		Options: []string{"user", "service", "all"},
		Default: viper.GetString("session_target"),
		Help:    "Which session(s) should be targeted for throttling?",
	}, &sessionTarget)

	// GPU throttling
	var gpuThrottling bool
	survey.AskOne(&survey.Confirm{
		Message: "Enable GPU Throttling?",
		Default: viper.GetBool("gpu_throttling"),
		Help:    "Throttle GPU usage to save power.",
	}, &gpuThrottling)

	// Affinity mask
	var affinityMask string
	survey.AskOne(&survey.Input{
		Message: "Set Affinity Mask (hex):",
		Default: viper.GetString("throttled_affinity_mask"),
		Help:    "CPU core mask in hex, e.g., 0xFF",
	}, &affinityMask)

	// Bypass list
	var bypassList string
	survey.AskOne(&survey.Input{
		Message: "Edit Bypass List (comma-separated):",
		Default: viper.GetString("bypass_list"),
		Help:    "Processes to never throttle, e.g., chrome.exe,vlc.exe",
	}, &bypassList)

	// Set values
	viper.Set("session_target", sessionTarget)
	viper.Set("gpu_throttling", gpuThrottling)
	viper.Set("throttled_affinity_mask", affinityMask)
	viper.Set("bypass_list", bypassList)

	err = viper.WriteConfigAs("config.yaml")
	if err != nil {
		fmt.Println("Error saving config:", err)
		os.Exit(1)
	}
	fmt.Println("Configuration saved successfully as config.yaml.")
}
