// Package models holds the trained models, built into the binary.
//
// They are written by the training repository (backend-v2: python -m pipeline.export):
//
//	meta.json            feature lists, the linear solar/demand model, house constants
//	price_lead<N>.json   XGBoost price model, N five-minute steps ahead (P10/P50/P90)
//	noise_pv.bin         the house's seeded 5-minute noise (float64, little-endian)
//	noise_load.bin
package models

import "embed"

//go:embed meta.json price_lead*.json noise_pv.bin noise_load.bin
var FS embed.FS
