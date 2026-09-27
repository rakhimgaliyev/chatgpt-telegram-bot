package config

import "testing"

func TestParseDotEnv(t *testing.T) {
	content := "# comment\n" +
		"export A=1\n" +
		"B=\"quoted\"\n" +
		"C='single'\n" +
		"PROMPT=\"line one.\n" +
		"line two with = sign.\n" +
		"line three.\"\n" +
		"D=after\n"

	got := parseDotEnv(content)
	want := map[string]string{
		"A":      "1",
		"B":      "quoted",
		"C":      "single",
		"PROMPT": "line one.\nline two with = sign.\nline three.",
		"D":      "after",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %#v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
