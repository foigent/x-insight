//go:build !linux

package config

import "os"

func validManagedSourceCACopy(string, os.FileInfo, *os.File) bool { return false }

func validSourceFactsSystemdCredential(_ string, _ os.FileInfo, _ *os.File) bool { return false }
func validReviewBodySystemdCredential(_ string, _ os.FileInfo, _ *os.File) bool  { return false }

func ValidAccessAccountSystemdCredential(_ string, _ os.FileInfo, _ *os.File) bool { return false }
