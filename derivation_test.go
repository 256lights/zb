// Copyright 2024 Ross Light
// SPDX-License-Identifier: MIT

package zb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/zb/internal/sortedset"
)

func TestDerivationMarshalText(t *testing.T) {
	tests := []struct {
		name     string
		drv      *Derivation
		want     []byte
		wantPath nix.StorePath
	}{
		{
			name: "FloatingCA",
			drv: &Derivation{
				Dir:     nix.DefaultStoreDirectory,
				Name:    "hello",
				System:  "x86_64-linux",
				Builder: "/bin/sh",
				Args:    []string{"-c", "echo 'Hello' > $out"},
				Env: map[string]string{
					"builder":        "/bin/sh",
					"name":           "hello",
					"out":            "/1rz4g4znpzjwh1xymhjpm42vipw92pr73vdgl6xs1hycac8kf2n9",
					"outputHashAlgo": "sha256",
					"outputHashMode": "recursive",
					"system":         "x86_64-linux",
				},
				Outputs: map[string]*DerivationOutput{
					"out": RecursiveFileFloatingCAOutput(nix.SHA256),
				},
			},

			wantPath: "/nix/store/cs4n5mbm46xwzb9yxm983gzqh0k5b2hp-hello.drv",
			want:     readTestdata(t, "cs4n5mbm46xwzb9yxm983gzqh0k5b2hp-hello.drv"),
		},
		{
			name: "FixedOutput",
			drv: &Derivation{
				Dir:     nix.DefaultStoreDirectory,
				Name:    "automake-1.16.5.tar.xz",
				System:  "x86_64-linux",
				Builder: "/nix/store/1b9p07z77phvv2hf6gm9f28syp39f1ag-bash-5.1-p16/bin/bash",
				Args: []string{
					"-e",
					"/nix/store/lphxcbw5wqsjskipaw1fb8lcf6pm6ri6-builder.sh",
				},
				Env: map[string]string{
					"SSL_CERT_FILE":               "/no-cert-file.crt",
					"buildInputs":                 "",
					"builder":                     "/nix/store/1b9p07z77phvv2hf6gm9f28syp39f1ag-bash-5.1-p16/bin/bash",
					"cmakeFlags":                  "",
					"configureFlags":              "",
					"curlOpts":                    "",
					"curlOptsList":                "",
					"depsBuildBuild":              "",
					"depsBuildBuildPropagated":    "",
					"depsBuildTarget":             "",
					"depsBuildTargetPropagated":   "",
					"depsHostHost":                "",
					"depsHostHostPropagated":      "",
					"depsTargetTarget":            "",
					"depsTargetTargetPropagated":  "",
					"doCheck":                     "",
					"doInstallCheck":              "",
					"downloadToTemp":              "",
					"executable":                  "",
					"impureEnvVars":               "http_proxy https_proxy ftp_proxy all_proxy no_proxy NIX_CURL_FLAGS NIX_HASHED_MIRRORS NIX_CONNECT_TIMEOUT NIX_MIRRORS_alsa NIX_MIRRORS_apache NIX_MIRRORS_bioc NIX_MIRRORS_bitlbee NIX_MIRRORS_centos NIX_MIRRORS_cpan NIX_MIRRORS_debian NIX_MIRRORS_fedora NIX_MIRRORS_gcc NIX_MIRRORS_gentoo NIX_MIRRORS_gnome NIX_MIRRORS_gnu NIX_MIRRORS_gnupg NIX_MIRRORS_hackage NIX_MIRRORS_hashedMirrors NIX_MIRRORS_ibiblioPubLinux NIX_MIRRORS_imagemagick NIX_MIRRORS_kde NIX_MIRRORS_kernel NIX_MIRRORS_luarocks NIX_MIRRORS_maven NIX_MIRRORS_mozilla NIX_MIRRORS_mysql NIX_MIRRORS_openbsd NIX_MIRRORS_opensuse NIX_MIRRORS_osdn NIX_MIRRORS_postgresql NIX_MIRRORS_pypi NIX_MIRRORS_qt NIX_MIRRORS_roy NIX_MIRRORS_sageupstream NIX_MIRRORS_samba NIX_MIRRORS_savannah NIX_MIRRORS_sourceforge NIX_MIRRORS_steamrt NIX_MIRRORS_tcsh NIX_MIRRORS_testpypi NIX_MIRRORS_ubuntu NIX_MIRRORS_xfce NIX_MIRRORS_xorg",
					"mesonFlags":                  "",
					"mirrorsFile":                 "/nix/store/2pm0lfi03anfdvrn5vb2n0jv4jfs7nb6-mirrors-list",
					"name":                        "automake-1.16.5.tar.xz",
					"nativeBuildInputs":           "/nix/store/jkp0ww7d1b62lkb4xc8nwhxx0iga9nqq-curl-7.84.0-dev",
					"nixpkgsVersion":              "22.11",
					"out":                         "/nix/store/gmaq49vzfrkvr714y4fhfxv100ijihin-automake-1.16.5.tar.xz",
					"outputHash":                  "0sdl32qxdy7m06iggmkkvf7j520rmmgbsjzbm7fgnxwxdp6mh7gh",
					"outputHashAlgo":              "sha256",
					"outputHashMode":              "flat",
					"outputs":                     "out",
					"patches":                     "",
					"postFetch":                   "",
					"preferHashedMirrors":         "1",
					"preferLocalBuild":            "1",
					"propagatedBuildInputs":       "",
					"propagatedNativeBuildInputs": "",
					"showURLs":                    "",
					"stdenv":                      "/nix/store/p93ivxvrf3c2w02la2c6nppmkgdh08y3-stdenv-linux",
					"strictDeps":                  "",
					"system":                      "x86_64-linux",
					"urls":                        "mirror://gnu/automake/automake-1.16.5.tar.xz",
				},
				InputDerivations: map[nix.StorePath]*sortedset.Set[string]{
					"/nix/store/6pj63b323pn53gpw3l5kdh1rly55aj15-bash-5.1-p16.drv": sortedset.New("out"),
					"/nix/store/8kd1la3xqfzdcb3gsgpp3k98m7g3hw9d-curl-7.84.0.drv":  sortedset.New("dev"),
					"/nix/store/g3m3mdgfsix265c945ncaxyyvx4cnx14-mirrors-list.drv": sortedset.New("out"),
					"/nix/store/zq638s1j77mxzc52ql21l9ncl3qsjb2h-stdenv-linux.drv": sortedset.New("out"),
				},
				InputSources: *sortedset.New[nix.StorePath](
					"/nix/store/lphxcbw5wqsjskipaw1fb8lcf6pm6ri6-builder.sh",
				),
				Outputs: map[string]*DerivationOutput{
					"out": FixedCAOutput(nix.FlatFileContentAddress(mustParseHash(t, "sha256:f01d58cd6d9d77fbdca9eb4bbd5ead1988228fdb73d6f7a201f5f8d6b118b469"))),
				},
			},

			want:     readTestdata(t, "0006yk8jxi0nmbz09fq86zl037c1wx9b-automake-1.16.5.tar.xz.drv"),
			wantPath: "/nix/store/0006yk8jxi0nmbz09fq86zl037c1wx9b-automake-1.16.5.tar.xz.drv",
		},
	}

	t.Run("MarshalText", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got, err := test.drv.MarshalText()
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(test.want, got); diff != "" {
					t.Errorf("drv.MarshalText() (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("StorePath", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got, err := test.drv.StorePath()
				if err != nil {
					t.Fatal(err)
				}
				if got != test.wantPath {
					t.Errorf("drv.StorePath() = %q; want %q", got, test.wantPath)
				}
			})
		}
	})
}

func TestDerivationOutputPath(t *testing.T) {
	tests := []struct {
		name       string
		out        *DerivationOutput
		drvName    string
		outputName string
		want       nix.StorePath
	}{
		{
			name:       "Text",
			out:        FixedCAOutput(nix.TextContentAddress(hashString(nix.SHA256, "Hello, World!\n"))),
			drvName:    "hello.txt",
			outputName: "out",
			want:       "/nix/store/q4dz47g15qmlsm01aijr737w8avkaac6-hello.txt",
		},
		{
			name:       "FlatFile",
			out:        FixedCAOutput(nix.FlatFileContentAddress(hashString(nix.SHA256, "Hello, World!\n"))),
			drvName:    "hello.txt",
			outputName: "out",
			want:       "/nix/store/22lrzcnq9ch2f3sz8d2idrm9gn72vcy2-hello.txt",
		},
		{
			name:       "RecursiveFile",
			out:        FixedCAOutput(nix.RecursiveFileContentAddress(helloNARHash(t))),
			drvName:    "hello.txt",
			outputName: "out",
			want:       "/nix/store/8dh7w49x7r3xkwz39vavcq6znygmzrp0-hello.txt",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := test.out.Path(nix.DefaultStoreDirectory, test.drvName, test.outputName)
			wantOK := test.want != ""
			if got != test.want || ok != wantOK {
				t.Errorf("out.Path(%q, %q, %q) = %q, %t; want %q, %t",
					nix.DefaultStoreDirectory, test.drvName, test.outputName, got, ok, test.want, wantOK)
			}
		})
	}
}

func helloNARHash(tb testing.TB) nix.Hash {
	h := nix.NewHasher(nix.SHA256)
	w := nar.NewWriter(h)
	const content = "Hello, World!\n"
	err := w.WriteHeader(&nar.Header{
		Size: int64(len(content)),
	})
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		tb.Fatal(err)
	}
	if err := w.Close(); err != nil {
		tb.Fatal(err)
	}
	return h.SumHash()
}

func readTestdata(tb testing.TB, name string) []byte {
	data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

func mustParseHash(tb testing.TB, s string) nix.Hash {
	h, err := nix.ParseHash(s)
	if err != nil {
		tb.Fatal(err)
	}
	return h
}

func hashString(typ nix.HashType, s string) nix.Hash {
	h := nix.NewHasher(typ)
	h.WriteString(s)
	return h.SumHash()
}
