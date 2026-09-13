package main

type PDTopology string

const (
	SingleNode      PDTopology = "single_node"
	MultiNodePrefill PDTopology = "multi_prefill"
	MultiNodeDecode  PDTopology = "multi_decode"
	HybridTopology   PDTopology = "hybrid"
)

type KVTransferConfig struct {
	KVConnector    string `json:"kv_connector"`
	KVRole         string `json:"kv_role"`
	KVBufferSize   string `json:"kv_buffer_size"`
	PrefillOnly    bool   `json:"prefill_only,omitempty"`
	DecodeOnly     bool   `json:"decode_only,omitempty"`
}

func PrefillConfig(connector string) KVTransferConfig {
	if connector == "" {
		connector = "mori_io"
	}
	return KVTransferConfig{
		KVConnector:  connector,
		KVRole:       "producer",
		KVBufferSize: "4GB",
		PrefillOnly:  true,
	}
}

func DecodeConfig(connector string) KVTransferConfig {
	if connector == "" {
		connector = "mori_io"
	}
	return KVTransferConfig{
		KVConnector:  connector,
		KVRole:       "consumer",
		KVBufferSize: "4GB",
		DecodeOnly:   true,
	}
}

type PDMetrics struct {
	KVTransferMs   float64
	GoodputSpeedup float64
	TTFTMs         float64
	TPOTMs         float64
}

func EstimatePDMetrics(topology PDTopology, seqLen int) PDMetrics {
	base := PDMetrics{
		KVTransferMs:   12.0,
		GoodputSpeedup: 2.5,
		TTFTMs:         2500,
		TPOTMs:         20,
	}
	switch topology {
	case SingleNode:
		base.KVTransferMs = 2.0
		base.GoodputSpeedup = 1.8
	case MultiNodePrefill, MultiNodeDecode:
		base.KVTransferMs = 15.0
		base.GoodputSpeedup = 2.2
	case HybridTopology:
		base.KVTransferMs = 8.0
		base.GoodputSpeedup = 2.5
	}
	base.TTFTMs = 800 + float64(seqLen)*1.2
	if base.TTFTMs > 2500 {
		base.TTFTMs = 2500
	}
	return base
}

func MigrateKV(kvData []byte, fromHW, toHW, modelID, kvFormat string) []byte {
	migrated := make([]byte, len(kvData))
	copy(migrated, kvData)
	if fromHW != toHW {
		for i := range migrated {
			migrated[i] ^= byte(len(modelID) + len(kvFormat))
		}
	}
	return migrated
}

func ShouldUsePD(numGPUs int, seqLen int) bool {
	if numGPUs >= 8 && seqLen > 1024 {
		return true
	}
	if numGPUs >= 4 && seqLen > 4096 {
		return true
	}
	return false
}
