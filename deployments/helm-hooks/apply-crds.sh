#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Using --force-conflicts to claim ownership of the CRDs from helm
kubectl apply --server-side=true --force-conflicts -f /internal-crds
