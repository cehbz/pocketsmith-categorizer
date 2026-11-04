package main

import (
	"errors"
	"strings"
	"testing"
)

func TestTheConsentPageOpensInTheBrowserAndIsPrinted(t *testing.T) {
	var out strings.Builder
	var opened []string
	showConsent(&out, func(url string) error { opened = append(opened, url); return nil })("https://consent.example/x")
	if len(opened) != 1 || opened[0] != "https://consent.example/x" {
		t.Errorf("opened %q; want the consent URL once", opened)
	}
	if !strings.Contains(out.String(), "https://consent.example/x") || !strings.Contains(out.String(), "Waiting for your consent") {
		t.Errorf("printed %q; want the URL and the wait", out.String())
	}
}

func TestABrowserThatFailsToOpenIsReportedAndTheURLStillPrinted(t *testing.T) {
	var out strings.Builder
	showConsent(&out, func(string) error { return errors.New("no browser") })("https://consent.example/x")
	if !strings.Contains(out.String(), "no browser") || !strings.Contains(out.String(), "https://consent.example/x") {
		t.Errorf("printed %q; want the browser's error and the URL", out.String())
	}
}
