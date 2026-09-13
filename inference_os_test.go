package main

import (
	"testing"
)

func TestHKVDEstimation(t *testing.T) {
	frags := []Fragment{
		{Enrichment: "self_contained"},
		{Enrichment: "summarized"},
		{Enrichment: "raw"},
	}
	hkvd := estimateHKVD(frags)
	if hkvd < 0.05 || hkvd > 0.25 {
		t.Errorf("HKVD out of bounds: %f", hkvd)
	}
	frags2 := []Fragment{{Enrichment: "raw"}, {Enrichment: "raw"}}
	hkvd2 := estimateHKVD(frags2)
	if hkvd > hkvd2 {
		t.Errorf("self_contained should not increase HKVD: %f vs %f", hkvd, hkvd2)
	}
}

func TestCacheBlendHKVD(t *testing.T) {
	s := seed()
	hkvd := hkvdForStore(s)
	if hkvd < 0.05 || hkvd > 0.25 {
		t.Errorf("HKVD %f out of bounds", hkvd)
	}
	blend := cacheBlendHKVD(s)
	if blend < 0.05 || blend > 0.25 {
		t.Errorf("CacheBlend HKVD %f out of bounds", blend)
	}
}

func TestHardwareProfiles(t *testing.T) {
	if _, ok := HardwareProfiles["h100_80gb"]; !ok {
		t.Error("missing h100 profile")
	}
	frac := chooseHKVDFraction("7b", "h100_80gb", 512)
	if frac < 0.15 || frac > 0.25 {
		t.Errorf("HKVD fraction %f out of quality bounds", frac)
	}
}

func TestEmbeddingProvider(t *testing.T) {
	prov := embeddingProvider()
	if prov != "hash" && prov != "jina" && prov != "cohere" {
		t.Errorf("unknown provider %s", prov)
	}
}
