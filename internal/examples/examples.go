// Package examples serves the curated showcase sites bundled with the API.
// The sites are static HTML snapshots adapted from verified aibench output;
// they are never generated at request time and never touch the AI provider.
package examples

import (
	"embed"
	"fmt"
)

//go:embed sites/*.html
var siteFiles embed.FS

// Label is the mandatory public label for every showcase example.
const Label = "Contoh hasil"

// Site is the public metadata for one showcase example.
type Site struct {
	Slug        string `json:"slug"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Label       string `json:"label"`
}

var catalog = []Site{
	{
		Slug:        "kuliner",
		Category:    "kuliner",
		Title:       "Kopi Arum",
		Description: "Kedai kopi dengan menu sederhana, jam buka, dan tombol pesan WhatsApp.",
		Label:       Label,
	},
	{
		Slug:        "jasa",
		Category:    "jasa",
		Title:       "Bengkel Jaya Motor",
		Description: "Halaman bengkel dengan daftar layanan, jam operasional, dan reservasi WhatsApp.",
		Label:       Label,
	},
	{
		Slug:        "toko",
		Category:    "toko",
		Title:       "Toko Melati",
		Description: "Toko peralatan rumah dengan katalog produk, cerita brand, dan cara pesan.",
		Label:       Label,
	},
	{
		Slug:        "portofolio",
		Category:    "portofolio",
		Title:       "Ruang Karya",
		Description: "Portofolio studio kreatif dengan layanan, prinsip kerja, dan ajakan menghubungi.",
		Label:       Label,
	},
	{
		Slug:        "acara",
		Category:    "acara",
		Title:       "Pesta Rasa",
		Description: "Halaman festival dengan jadwal, daftar tenant, lokasi, dan pendaftaran.",
		Label:       Label,
	},
}

// List returns a copy of the example catalog in display order.
func List() []Site {
	out := make([]Site, len(catalog))
	copy(out, catalog)
	return out
}

// Get returns the metadata and HTML for slug. The slug must match the
// whitelist exactly, so callers cannot traverse the embedded filesystem.
func Get(slug string) (Site, []byte, bool) {
	for _, site := range catalog {
		if site.Slug != slug {
			continue
		}
		html, err := siteFiles.ReadFile(fmt.Sprintf("sites/%s.html", site.Slug))
		if err != nil {
			return Site{}, nil, false
		}
		return site, html, true
	}
	return Site{}, nil, false
}
