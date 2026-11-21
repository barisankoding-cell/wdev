package site

import "testing"

func TestValidateDomain(t *testing.T) {
	valid := []string{"app.test", "my-site.local", "sub.example.com", "a1.b2.io"}
	for _, d := range valid {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) unexpected error: %v", d, err)
		}
	}

	invalid := []string{
		"",
		"../etc/nginx",
		"app.test'; system('id'); '",
		"app.test;\nlocation / {}",
		"-flag.test",
		"localhost",
		"a/b.test",
	}
	for _, d := range invalid {
		if err := ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) expected error, got nil", d)
		}
	}
}

func TestValidateRootPath(t *testing.T) {
	valid := []string{"/var/www/app.test", "/home/user/projects/app", "/srv/web"}
	for _, p := range valid {
		if _, err := ValidateRootPath(p); err != nil {
			t.Errorf("ValidateRootPath(%q) unexpected error: %v", p, err)
		}
	}

	invalid := []string{
		"",
		"relative/path",
		"/etc/nginx",
		"/var/www/../../etc/shadow",
		"/var/www/app; include /etc/passwd;",
		"/var/www/app\nmalicious",
	}
	for _, p := range invalid {
		if _, err := ValidateRootPath(p); err == nil {
			t.Errorf("ValidateRootPath(%q) expected error, got nil", p)
		}
	}
}
