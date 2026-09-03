// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package imagelock

import (
	"strings"
	"testing"
)

// renderedManifest carries every shape the collector has to understand: a pod spec
// image, a custom resource embedded as ConfigMap text whose images an operator
// creates later, an image key that is not called "image", and a shell script that
// must not be mistaken for a manifest.
const renderedManifest = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: krm-operator
spec:
  template:
    spec:
      containers:
        - name: krm-operator
          image: example.test/krm/krm-operator:v1.2.3
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: kai-config
data:
  kai-config.yaml: |
    apiVersion: kai.scheduler/v1
    kind: Config
    spec:
      scheduler:
        service:
          image:
            name: scheduler
            repository: example.test/kai
            tag: v0.17.0
      nodeScaleAdjuster:
        args:
          scalingPodImage:
            name: scalingpod
            repository: example.test/kai
            tag: v0.17.0
  entrypoint.sh: |
    #!/bin/bash
    set -euo pipefail
    kubectl apply -f /crds
---
apiVersion: batch/v1
kind: Job
metadata:
  name: crd-upgrader
spec:
  template:
    spec:
      containers:
        - name: crd-upgrader
          image: "example.test/kai/crd-upgrader:v0.17.0"
`

// testCatalog locks the chart's own registry and leaves the subchart's to the
// project that publishes it, mirroring the real split.
func testCatalog() imageCatalog {
	return imageCatalog{lock: "example.test/krm", skip: "example.test/kai"}
}

func TestImageRefsFindsEverySpelling(t *testing.T) {
	refs, err := imageRefs([]byte(renderedManifest))
	if err != nil {
		t.Fatalf("imageRefs: %v", err)
	}

	want := []string{
		"example.test/kai/crd-upgrader:v0.17.0",
		"example.test/kai/scalingpod:v0.17.0",
		"example.test/kai/scheduler:v0.17.0",
		"example.test/krm/krm-operator:v1.2.3",
	}
	if len(refs) != len(want) {
		t.Fatalf("got %d references %v, want %d %v", len(refs), refs, len(want), want)
	}
	for i, ref := range want {
		if refs[i] != ref {
			t.Errorf("reference %d: got %q, want %q", i, refs[i], ref)
		}
	}
}

// The lock covers only what this repository builds; the subchart's images are
// recognised, counted and left to the project that publishes them.
func TestImagesFromManifestLocksOnlyThisRepositorysImages(t *testing.T) {
	images, skipped, err := imagesFromManifest([]byte(renderedManifest), testCatalog())
	if err != nil {
		t.Fatalf("imagesFromManifest: %v", err)
	}

	want := []chartImage{
		{name: "krm-operator", repo: "example.test/krm/krm-operator", tag: "v1.2.3"},
	}
	if len(images) != len(want) {
		t.Fatalf("got %d images %v, want %d", len(images), images, len(want))
	}
	for i := range want {
		if images[i] != want[i] {
			t.Errorf("image %d: got %+v, want %+v", i, images[i], want[i])
		}
	}
	if skipped != 3 {
		t.Errorf("got %d skipped images, want the 3 the subchart runs", skipped)
	}
}

// A third-party image belongs to no release's lock until someone decides it does,
// so it has to stop the run rather than be skipped like the subchart's.
func TestImagesFromManifestRejectsAnUnownedImage(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Pod
spec:
  containers:
    - image: registry.k8s.io/kubectl:v1.34.0
`
	_, _, err := imagesFromManifest([]byte(manifest), testCatalog())
	if err == nil {
		t.Fatal("expected an image under neither registry to fail the render")
	}
	if !strings.Contains(err.Error(), "registry.k8s.io/kubectl") {
		t.Errorf("error should name the repository, got: %v", err)
	}
}

func TestCatalogClassify(t *testing.T) {
	catalog := testCatalog()
	tests := []struct {
		repo   string
		name   string
		locked bool
		fails  bool
	}{
		{repo: "example.test/krm/krm-operator", name: "krm-operator", locked: true},
		{repo: "example.test/kai/scheduler"},
		{repo: "registry.k8s.io/kubectl", fails: true},
		// The boundary is a path separator, so a registry whose name merely prefixes
		// ours is not mistaken for it.
		{repo: "example.test/krm-staging/krm-operator", fails: true},
	}
	for _, test := range tests {
		t.Run(test.repo, func(t *testing.T) {
			name, locked, err := catalog.classify(test.repo)
			if (err != nil) != test.fails {
				t.Fatalf("got error %v, want failure=%v", err, test.fails)
			}
			if name != test.name || locked != test.locked {
				t.Errorf("got (%q, %v), want (%q, %v)", name, locked, test.name, test.locked)
			}
		})
	}
}

func TestImagesFromManifestRejectsOneNameWithTwoTags(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Pod
spec:
  containers:
    - image: example.test/krm/krm-operator:v1.2.3
    - image: example.test/krm/krm-operator:v1.2.4
`
	_, _, err := imagesFromManifest([]byte(manifest), testCatalog())
	if err == nil {
		t.Fatal("expected two tags of one image to be refused")
	}
	if !strings.Contains(err.Error(), "v1.2.3") || !strings.Contains(err.Error(), "v1.2.4") {
		t.Errorf("error should name both references, got: %v", err)
	}
}

func TestImageReference(t *testing.T) {
	tests := []struct {
		name string
		node any
		want string
		ok   bool
	}{
		{name: "string", node: "example.test/a:v1", want: "example.test/a:v1", ok: true},
		{name: "empty string", node: ""},
		{name: "structured", node: map[string]any{"name": "a", "repository": "example.test", "tag": "v1"}, want: "example.test/a:v1", ok: true},
		{name: "structured without repository", node: map[string]any{"name": "a", "tag": "v1"}, want: "a:v1", ok: true},
		{name: "structured without tag", node: map[string]any{"name": "a", "repository": "example.test"}},
		{name: "unrelated map", node: map[string]any{"pullPolicy": "IfNotPresent"}},
		{name: "number", node: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := imageReference(test.node)
			if ok != test.ok || got != test.want {
				t.Errorf("got (%q, %v), want (%q, %v)", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestSplitReference(t *testing.T) {
	tests := []struct {
		ref  string
		repo string
		tag  string
	}{
		{ref: "ghcr.io/org/name:v1.2.3", repo: "ghcr.io/org/name", tag: "v1.2.3"},
		{ref: "localhost:5000/name:v1", repo: "localhost:5000/name", tag: "v1"},
		{ref: "localhost:5000/name", repo: "localhost:5000/name"},
		{ref: "name", repo: "name"},
	}
	for _, test := range tests {
		t.Run(test.ref, func(t *testing.T) {
			repo, tag := splitReference(test.ref)
			if repo != test.repo || tag != test.tag {
				t.Errorf("got (%q, %q), want (%q, %q)", repo, tag, test.repo, test.tag)
			}
		})
	}
}

func TestNewImageCatalogFollowsTheReleaseRegistry(t *testing.T) {
	catalog := newImageCatalog("example.test/krm")

	if catalog.lock != "example.test/krm" {
		t.Errorf("the lock registry is the one passed in, got %q", catalog.lock)
	}
	// kai-scheduler keeps its own pinned registry, whatever this release publishes to.
	if catalog.skip != kaiRegistry {
		t.Errorf("got skip registry %q, want %q", catalog.skip, kaiRegistry)
	}
	if _, locked, err := catalog.classify(kaiRegistry + "/scheduler"); err != nil || locked {
		t.Errorf("kai-scheduler images must be recognised and left out, got locked=%v err=%v", locked, err)
	}
}

func TestIsImageKey(t *testing.T) {
	for _, key := range []string{"image", "scalingPodImage"} {
		if !isImageKey(key) {
			t.Errorf("%q should be treated as an image key", key)
		}
	}
	for _, key := range []string{"images", "imagePullPolicy", "name"} {
		if isImageKey(key) {
			t.Errorf("%q should not be treated as an image key", key)
		}
	}
}
