// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"sort"
	"strings"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// kaiRegistry is the bundled subchart's registry. Unlike the chart's own it is
// never rewritten at release time: values.yaml pins it so a parent override
// cannot clobber it.
const kaiRegistry = "ghcr.io/kai-scheduler/kai-scheduler"

type chartImage struct {
	name string // short name the lock lists it under
	repo string // repository, without the tag
	tag  string
}

func (c chartImage) reference() string { return c.repo + ":" + c.tag }

// imageCatalog decides who owns an image, and therefore who locks it. Ownership
// follows the registry rather than a list of image names, because both registries
// are stable whereas names would have to be restated here whenever either chart
// gained an image.
type imageCatalog struct {
	// lock is where this release publishes, so a service added to the chart is
	// covered without a change here.
	lock string
	// skip is the bundled subchart's registry: recognised so it does not trip
	// classify, excluded because the kai-scheduler project locks it itself.
	skip string
}

func newImageCatalog(registry string) imageCatalog {
	return imageCatalog{lock: registry, skip: kaiRegistry}
}

// classify reports the name an image gets and whether it belongs in the lock. An
// image under neither registry stops the run rather than vanishing quietly: a
// third-party image reaches an air-gapped site only if someone puts it there.
func (c imageCatalog) classify(repo string) (name string, locked bool, err error) {
	switch {
	case under(repo, c.lock):
		return path.Base(repo), true, nil
	case under(repo, c.skip):
		return "", false, nil
	default:
		return "", false, fmt.Errorf(
			"image %q is published by neither this release (%s) nor the bundled kai-scheduler (%s); "+
				"decide which lock covers it before releasing", repo, c.lock, c.skip)
	}
}

// under matches on a path boundary, so a registry is not confused with one whose
// name it prefixes.
func under(repo, registry string) bool {
	return strings.HasPrefix(repo, registry+"/")
}

// renderChart forces no toggles on: the chart's defaults already name every image
// an install can run, since a component switched off still carries its image block
// into the KRMConfig or the KAI Config.
func renderChart(ctx context.Context, opts options, prof profile) ([]byte, error) {
	args := []string{"template", "krm", opts.chart,
		"--set", "image.registry=" + opts.registry,
		"--set", "image.tag=" + opts.version,
	}
	if prof == profileFIPS {
		// Two keys: Helm shares global.* into subcharts verbatim, and the pinned
		// kai-scheduler release spells the same switch as a boolean.
		args = append(args, "--set", "global.fipsMode=on", "--set", "kai-scheduler.global.fips=true")
	}

	helmBin := opts.helmBin
	if helmBin == "" {
		helmBin = "helm"
	}
	helm := exec.CommandContext(ctx, helmBin, args...)
	var stderr bytes.Buffer
	helm.Stderr = &stderr
	rendered, err := helm.Output()
	if err != nil {
		return nil, fmt.Errorf("helm template: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return rendered, nil
}

// imagesFromManifest returns the images this release locks, and how many belong
// to another project's lock.
func imagesFromManifest(manifest []byte, catalog imageCatalog) (images []chartImage, skipped int, err error) {
	refs, err := imageRefs(manifest)
	if err != nil {
		return nil, 0, err
	}

	byName := map[string]chartImage{}
	for _, ref := range refs {
		repo, tag := splitReference(ref)
		name, locked, err := catalog.classify(repo)
		if err != nil {
			return nil, 0, err
		}
		if !locked {
			skipped++
			continue
		}
		if existing, seen := byName[name]; seen && existing.reference() != ref {
			return nil, 0, fmt.Errorf("image %q maps to two references: %s and %s", name, existing.reference(), ref)
		}
		byName[name] = chartImage{name: name, repo: repo, tag: tag}
	}

	images = make([]chartImage, 0, len(byName))
	for _, image := range byName {
		images = append(images, image)
	}
	sort.Slice(images, func(i, j int) bool { return images[i].name < images[j].name })
	return images, skipped, nil
}

func imageRefs(manifest []byte) ([]string, error) {
	refs := map[string]struct{}{}
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(manifest)))
	for {
		document, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("split rendered manifest: %w", err)
		}
		if len(bytes.TrimSpace(document)) == 0 {
			continue
		}
		var node any
		if err := yaml.Unmarshal(document, &node); err != nil {
			return nil, fmt.Errorf("parse rendered document: %w", err)
		}
		collectImages(node, refs)
		collectEmbeddedImages(node, refs)
	}

	unique := make([]string, 0, len(refs))
	for ref := range refs {
		unique = append(unique, ref)
	}
	sort.Strings(unique)
	return unique, nil
}

// collectImages handles both spellings: a pod spec's `image: repo:tag`, and the
// {name, repository, tag} object the KAI and KRM operators assemble.
func collectImages(node any, refs map[string]struct{}) {
	switch typed := node.(type) {
	case map[string]any:
		for key, child := range typed {
			if isImageKey(key) {
				if ref, ok := imageReference(child); ok {
					refs[ref] = struct{}{}
					continue
				}
			}
			collectImages(child, refs)
		}
	case []any:
		for _, child := range typed {
			collectImages(child, refs)
		}
	}
}

// collectEmbeddedImages walks the custom resources the charts ship as ConfigMap
// text. The images their operators later create are named nowhere else.
func collectEmbeddedImages(node any, refs map[string]struct{}) {
	document, ok := node.(map[string]any)
	if !ok || document["kind"] != "ConfigMap" {
		return
	}
	data, ok := document["data"].(map[string]any)
	if !ok {
		return
	}
	for _, value := range data {
		text, ok := value.(string)
		if !ok {
			continue
		}
		var embedded any
		// Most entries are scripts; anything that does not decode holds no image.
		if err := yaml.Unmarshal([]byte(text), &embedded); err != nil {
			continue
		}
		collectImages(embedded, refs)
	}
}

// isImageKey accepts the suffix form for the KAI Config's scalingPodImage, which
// sits outside a service block.
func isImageKey(key string) bool {
	return key == "image" || strings.HasSuffix(key, "Image")
}

func imageReference(node any) (string, bool) {
	switch typed := node.(type) {
	case string:
		return typed, typed != ""
	case map[string]any:
		name, ok := typed["name"].(string)
		if !ok || name == "" {
			return "", false
		}
		tag, ok := typed["tag"].(string)
		if !ok || tag == "" {
			return "", false
		}
		if repository, ok := typed["repository"].(string); ok && repository != "" {
			return repository + "/" + name + ":" + tag, true
		}
		return name + ":" + tag, true
	}
	return "", false
}

// splitReference divides repo:tag. A colon after the last slash is a tag; before
// it, a registry port.
func splitReference(ref string) (repo, tag string) {
	slash := strings.LastIndexByte(ref, '/')
	if colon := strings.LastIndexByte(ref, ':'); colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, ""
}
