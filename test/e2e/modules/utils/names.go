// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package utils holds small helpers shared by the e2e suites.
package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateName returns a unique DNS-1123 name under prefix.
//
// Names have to be unique across specs, not merely within one: several of these
// objects are cluster-scoped, and the NodePool webhook rejects a second pool
// reusing a labelKey/labelValue pair. Ginkgo also randomises spec order by
// default, so a fixed name would fail in some orderings and not others.
func GenerateName(prefix string) string {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		panic("e2e: cannot generate a name: " + err.Error())
	}

	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(buf))
}
