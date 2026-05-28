package hosting

import "testing"

func TestDeploymentMode_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    DeploymentMode
		wantErr bool
	}{
		{name: "replacement", mode: DeploymentModeReplacement},
		{name: "overlay", mode: DeploymentModeOverlay},
		{name: "empty", wantErr: true},
		{name: "unknown", mode: DeploymentMode("incremental"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mode.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
