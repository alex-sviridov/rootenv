package main

import "testing"

func TestAssetConfigDef(t *testing.T) {
	cfg := &AssetConfig{
		Configuration: []byte(`{"image":"alpine","ssh_user":"lab","cpu":"1","memory":"128MB"}`),
	}
	def, err := cfg.Def()
	if err != nil {
		t.Fatalf("Def: %v", err)
	}
	if def.Image != "alpine" || def.SSHUser != "lab" || def.CPU != "1" || def.Memory != "128MB" {
		t.Errorf("Def fields wrong: %+v", def)
	}
}

func TestAssetConfigDefInvalidJSON(t *testing.T) {
	cfg := &AssetConfig{Configuration: []byte(`not json`)}
	if _, err := cfg.Def(); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestAssetDefValidate(t *testing.T) {
	cases := []struct {
		name string
		def  AssetDef
		ok   bool
	}{
		{"valid", AssetDef{Image: "alpine", SSHUser: "lab", CPU: "1", Memory: "128MB"}, true},
		{"missing image", AssetDef{SSHUser: "lab", CPU: "1", Memory: "128MB"}, false},
		{"missing ssh_user", AssetDef{Image: "alpine", CPU: "1", Memory: "128MB"}, false},
		{"missing cpu", AssetDef{Image: "alpine", SSHUser: "lab", Memory: "128MB"}, false},
		{"missing memory", AssetDef{Image: "alpine", SSHUser: "lab", CPU: "1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.validate()
			if tc.ok && err != nil {
				t.Errorf("want no error, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("want error, got nil")
			}
		})
	}
}
