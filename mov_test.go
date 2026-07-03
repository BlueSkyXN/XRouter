package main

import "testing"

func TestVerificationPassedRequiresExplicitJSONPass(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "boolean pass", text: `{"pass":true,"reason":"ok"}`, want: true},
		{name: "boolean fail with passed reason", text: `{"pass":false,"reason":"not passed"}`, want: false},
		{name: "substring only is not enough", text: "the answer passed verification", want: false},
		{name: "wrapped json", text: "```json\n{\"pass\": true}\n```", want: true},
		{name: "string true", text: `{"pass":"true"}`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verificationPassed(tt.text); got != tt.want {
				t.Fatalf("verificationPassed(%q)=%v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
