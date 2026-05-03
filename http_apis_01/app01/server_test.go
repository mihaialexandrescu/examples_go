package app01

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"
)

func TestGreet1(t *testing.T) {
	tcs := []struct {
		name      string
		greeting  string
		url       string
		method    string
		want_body string
		want_code int
	}{
		{
			name:      "simple Hello",
			greeting:  "Hello",
			url:       "/greet1",
			method:    http.MethodGet,
			want_body: "GET /greet1: Hello, world!\n",
			want_code: http.StatusOK,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, nil)
			w := httptest.NewRecorder()

			handleGreet1(tc.greeting).ServeHTTP(w, req)

			res := w.Result()
			defer res.Body.Close()

			data, err := io.ReadAll(res.Body)

			if err != nil {
				t.Errorf("want error read body to be nil, got %v", err)
			}

			if got := string(data); got != tc.want_body {
				t.Errorf("got %q, want %q", got, tc.want_body)
			}

			if got := res.StatusCode; got != tc.want_code {
				t.Errorf("got StatusCode %d, want %d", got, tc.want_code)
			}
		})
	}
}

func TestReadyz(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	handler := handleReadyz(logger)

	check := func(duration_want time.Duration) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		start := time.Now()
		handler.ServeHTTP(w, req)
		duration_got := time.Since(start)

		max_want := duration_want * 110 / 100 // Note: 10% extra variation
		t.Logf("Got duration: %v; want min: %v; want max: %v\n", duration_got, duration_want, max_want)
		if duration_got < duration_want || duration_got > max_want {
			t.Errorf("got duration %v, want at least %v", duration_got, duration_want)
		}

		res := w.Result()
		defer res.Body.Close()

		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Errorf("got error read body %v, wanted nil", err)
		}

		want_body := "{\"status\":\"ok\"}\n"
		if got := string(data); got != want_body {
			t.Errorf("got %q, want %q", got, want_body)
		}

		want_code := http.StatusOK
		if got := res.StatusCode; got != want_code {
			t.Errorf("got StatusCode %d, want %d", got, want_code)
		}
	}

	// This takes the test from 6.004s to 0.004s
	synctest.Test(t, func(t *testing.T) {
		check(5 * time.Second)
		check(1 * time.Second)
		check(1 * time.Second)
	})
}
