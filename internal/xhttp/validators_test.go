// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"net/http"
	"testing"
	"time"
)

func TestEvaluatePreconditions(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		requestHeader http.Header
		vf            ValidatorFields
		exists        bool
		want          int
	}{
		{
			name:   "NoRequestHeaders/Get",
			exists: true,
			want:   http.StatusOK,
		},
		{
			name:   "NoRequestHeaders/NotFound",
			exists: false,
			want:   http.StatusNotFound,
		},
		{
			name:   "NoRequestHeaders/PutOverwrite",
			method: http.MethodPut,
			exists: true,
			want:   http.StatusOK,
		},
		{
			name:   "NoRequestHeaders/PutCreate",
			method: http.MethodPut,
			exists: false,
			want:   http.StatusCreated,
		},
		{
			name:   "IfMatch/Zero",
			exists: true,
			requestHeader: http.Header{
				"If-Match": {`"xyzzy"`},
			},
			vf:   ValidatorFields{},
			want: http.StatusPreconditionFailed,
		},
		{
			name:   "IfMatch/Matches",
			exists: true,
			requestHeader: http.Header{
				"If-Match": {`"xyzzy"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusOK,
		},
		{
			name:   "IfMatch/MatchMultiple",
			exists: true,
			requestHeader: http.Header{
				"If-Match": {`"abcdef", "xyzzy"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusOK,
		},
		{
			name:   "IfMatch/MatchesPUT",
			method: http.MethodPut,
			exists: true,
			requestHeader: http.Header{
				"If-Match": {`"xyzzy"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusOK,
		},
		{
			name:   "IfMatch/DoesNotMatch",
			exists: true,
			requestHeader: http.Header{
				"If-Match": {`"bork"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusPreconditionFailed,
		},
		{
			name:   "IfMatch/Wildcard",
			exists: true,
			requestHeader: http.Header{
				"If-Match": {`*`},
			},
			want: http.StatusOK,
		},
		{
			name:   "IfMatch/WildcardNotExist",
			exists: false,
			requestHeader: http.Header{
				"If-Match": {`*`},
			},
			want: http.StatusPreconditionFailed,
		},
		{
			name:   "IfNoneMatch/Zero",
			exists: true,
			requestHeader: http.Header{
				"If-None-Match": {`"xyzzy"`},
			},
			vf:   ValidatorFields{},
			want: http.StatusOK,
		},
		{
			name:   "IfNoneMatch/Matches",
			exists: true,
			requestHeader: http.Header{
				"If-None-Match": {`"xyzzy"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusNotModified,
		},
		{
			name:   "IfNoneMatch/DoesNotMatch",
			exists: true,
			requestHeader: http.Header{
				"If-None-Match": {`"bork"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusOK,
		},
		{
			name:   "IfNoneMatch/MatchesPUT",
			method: http.MethodPut,
			exists: true,
			requestHeader: http.Header{
				"If-None-Match": {`"xyzzy"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusPreconditionFailed,
		},
		{
			name:   "IfNoneMatch/DoesNotMatchPut",
			method: http.MethodPut,
			exists: true,
			requestHeader: http.Header{
				"If-None-Match": {`"bork"`},
			},
			vf: ValidatorFields{
				ETag: `"xyzzy"`,
			},
			want: http.StatusOK,
		},
		{
			name:   "IfNoneMatch/WildcardPut",
			method: http.MethodPut,
			exists: true,
			requestHeader: http.Header{
				"If-None-Match": {`*`},
			},
			want: http.StatusPreconditionFailed,
		},
		{
			name:   "IfNoneMatch/WildcardNotExistPut",
			method: http.MethodPut,
			exists: false,
			requestHeader: http.Header{
				"If-None-Match": {`*`},
			},
			want: http.StatusCreated,
		},
		{
			name:   "IfModifiedSince/Zero",
			exists: true,
			requestHeader: http.Header{
				"If-Modified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf:   ValidatorFields{},
			want: http.StatusOK,
		},
		{
			name:   "IfModifiedSince/Modified",
			exists: true,
			requestHeader: http.Header{
				"If-Modified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.November, 1, 8, 0, 0, 0, time.UTC),
			},
			want: http.StatusOK,
		},
		{
			name:   "IfModifiedSince/Unmodified",
			exists: true,
			requestHeader: http.Header{
				"If-Modified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.October, 1, 8, 0, 0, 0, time.UTC),
			},
			want: http.StatusNotModified,
		},
		{
			name:   "IfModifiedSince/SameSecond",
			exists: true,
			requestHeader: http.Header{
				"If-Modified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.October, 29, 19, 43, 31, int(750*time.Millisecond/time.Nanosecond), time.UTC),
			},
			want: http.StatusNotModified,
		},
		{
			name:   "IfModifiedSince/UnmodifiedPut",
			method: http.MethodPut,
			exists: true,
			requestHeader: http.Header{
				"If-Modified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.October, 1, 8, 0, 0, 0, time.UTC),
			},
			want: http.StatusOK,
		},
		{
			name:   "IfUnmodifiedSince/Zero",
			exists: true,
			requestHeader: http.Header{
				"If-Unmodified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf:   ValidatorFields{},
			want: http.StatusOK,
		},
		{
			name:   "IfUnmodifiedSince/Modified",
			exists: true,
			requestHeader: http.Header{
				"If-Unmodified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.November, 1, 8, 0, 0, 0, time.UTC),
			},
			want: http.StatusPreconditionFailed,
		},
		{
			name:   "IfUnmodifiedSince/Unmodified",
			exists: true,
			requestHeader: http.Header{
				"If-Unmodified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.October, 1, 8, 0, 0, 0, time.UTC),
			},
			want: http.StatusOK,
		},
		{
			name:   "IfUnmodifiedSince/SameSecond",
			exists: true,
			requestHeader: http.Header{
				"If-Unmodified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.October, 29, 19, 43, 31, int(750*time.Millisecond/time.Nanosecond), time.UTC),
			},
			want: http.StatusOK,
		},
		{
			name:   "IfUnmodifiedSince/ModifiedPut",
			method: http.MethodPut,
			exists: true,
			requestHeader: http.Header{
				"If-Unmodified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.November, 1, 8, 0, 0, 0, time.UTC),
			},
			want: http.StatusPreconditionFailed,
		},
		{
			name:   "IfUnmodifiedSince/UnmodifiedPut",
			method: http.MethodPut,
			exists: true,
			requestHeader: http.Header{
				"If-Unmodified-Since": {"Sat, 29 Oct 1994 19:43:31 GMT"},
			},
			vf: ValidatorFields{
				LastModified: time.Date(1994, time.October, 1, 8, 0, 0, 0, time.UTC),
			},
			want: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluatePreconditions(test.method, test.requestHeader, test.vf, test.exists)
			if got != test.want {
				t.Errorf("EvaluatePreconditions(%s, %v, %v, %t) = %d (%s); want %d (%s)",
					test.method, test.requestHeader, test.vf, test.exists, got, http.StatusText(got), test.want, http.StatusText(test.want))
			}
		})
	}
}
