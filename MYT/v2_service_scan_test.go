package main

import "testing"

func TestV2ModelAllowed(t *testing.T) {
	// 白名单内（含 _v2 后缀与裸名）
	allowed := []string{"c1", "q1", "q1_10", "c1_10", "q1_v2", "r1s_v2", "c1_v2",
		"r1q_v2", "q1l_v2", "p1", "p1_10", "p1_v2", "m1_v2"}
	for _, name := range allowed {
		if !v2ModelAllowed(name) {
			t.Errorf("v2ModelAllowed(%q) = false, want true", name)
		}
	}

	// 大小写不敏感
	caseVariants := []string{"C1", "Q1_V2", "R1S_V2", "P1_10", "M1_V2"}
	for _, name := range caseVariants {
		if !v2ModelAllowed(name) {
			t.Errorf("v2ModelAllowed(%q) = false, want true（大小写不敏感）", name)
		}
	}

	// 首尾空白容忍
	if !v2ModelAllowed(" c1_v2 ") {
		t.Error(`v2ModelAllowed(" c1_v2 ") = false, want true（去首尾空白）`)
	}

	// 白名单外的裸名与 _v2 型号一律拒绝
	denied := []string{"x9", "q2", "z1_10", "r9s_v2", "abc_v2", "", "c1_v3", "r1s_v3"}
	for _, name := range denied {
		if v2ModelAllowed(name) {
			t.Errorf("v2ModelAllowed(%q) = true, want false", name)
		}
	}
}
