/*
Copyright © 2026 SUSE LLC
SPDX-License-Identifier: Apache-2.0

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package release

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
	"github.com/suse/elemental/v3/pkg/manifest/source"
)

type sourceReader struct {
	ctx context.Context
}

func (s sourceReader) Read(m *source.ReleaseManifestSource) ([]byte, error) {
	const manifestPath = "release_manifest.yaml"

	imageRef := m.URI()

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("parsing image reference %s: %w", imageRef, err)
	}

	img, err := remote.Image(ref,
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithTransport(http.DefaultTransport),
		remote.WithPlatform(v1.Platform{OS: runtime.GOOS, Architecture: runtime.GOARCH}),
		remote.WithContext(s.ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching remote image %s: %w", imageRef, err)
	}

	imageReadCloser := mutate.Extract(img)
	defer func() {
		_ = imageReadCloser.Close()
	}()

	tarReader := tar.NewReader(imageReadCloser)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("manifest file not found in image at path: %s", manifestPath)
			}
			return nil, fmt.Errorf("reading tar stream: %w", err)
		}

		if header.Name == manifestPath {
			manifestData, err := io.ReadAll(tarReader)
			if err != nil {
				return nil, fmt.Errorf("reading manifest file contents: %w", err)
			}
			return manifestData, nil
		}
	}
}

func RetrieveManifest(ctx context.Context, registry, version string) (*resolver.ResolvedManifest, error) {
	reader := sourceReader{ctx: ctx}
	r := resolver.New(reader)

	imageRef := fmt.Sprintf("%s:%s", registry, version)
	if !strings.HasPrefix(imageRef, "oci://") {
		imageRef = "oci://" + imageRef
	}

	return r.Resolve(imageRef)
}
