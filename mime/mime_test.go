package mime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttpmime "github.com/hueristiq/hq-lib-http-go/mime"
)

func TestMIME_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mime     hqgohttpmime.MIME
		expected string
	}{
		{"HTML", hqgohttpmime.HTML, "text/html"},
		{"JSON", hqgohttpmime.JSON, "application/json"},
		{"JSONLD", hqgohttpmime.JSONLD, "application/ld+json"},
		{"XML", hqgohttpmime.XML, "application/xml"},
		{"Text", hqgohttpmime.Text, "text/plain"},
		{"CSV", hqgohttpmime.CSV, "text/csv"},
		{"PNG", hqgohttpmime.PNG, "image/png"},
		{"JPEG", hqgohttpmime.JPEG, "image/jpeg"},
		{"GIF", hqgohttpmime.GIF, "image/gif"},
		{"PDF", hqgohttpmime.PDF, "application/pdf"},
		{"ZIPArchive", hqgohttpmime.ZIPArchive, "application/zip"},
		{"GZipCompressedArchive", hqgohttpmime.GZipCompressedArchive, "application/gzip"},
		{"MP4Video", hqgohttpmime.MP4Video, "video/mp4"},
		{"MP3Audio", hqgohttpmime.MP3Audio, "audio/mpeg"},
		{"WOFF2", hqgohttpmime.WOFF2, "font/woff2"},
		{"BinaryData", hqgohttpmime.BinaryData, "application/octet-stream"},
		{"MultipartFormData", hqgohttpmime.MultipartFormData, "multipart/form-data"},
		{"FormURLEncoded", hqgohttpmime.FormURLEncoded, "application/x-www-form-urlencoded"},
		{"EventStream", hqgohttpmime.EventStream, "text/event-stream"},
		{"JavaScript", hqgohttpmime.JavaScript, "text/javascript"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.mime.String())
		})
	}
}

func TestMIME_ConstantsAreUnique(t *testing.T) {
	t.Parallel()

	values := []hqgohttpmime.MIME{
		hqgohttpmime.AACAudio,
		hqgohttpmime.AbiWordDocument,
		hqgohttpmime.AmazonKindleEBook,
		hqgohttpmime.AppleInstallerPackage,
		hqgohttpmime.ArchiveDocument,
		hqgohttpmime.AVIFImage,
		hqgohttpmime.AVIVideo,
		hqgohttpmime.BinaryData,
		hqgohttpmime.BitmapImage,
		hqgohttpmime.BourneShellScript,
		hqgohttpmime.BZip2Archive,
		hqgohttpmime.BZipArchive,
		hqgohttpmime.CDAudio,
		hqgohttpmime.CShellScript,
		hqgohttpmime.CSS,
		hqgohttpmime.CSV,
		hqgohttpmime.EPUB,
		hqgohttpmime.EventStream,
		hqgohttpmime.FormURLEncoded,
		hqgohttpmime.GZipCompressedArchive,
		hqgohttpmime.GIF,
		hqgohttpmime.HTML,
		hqgohttpmime.ICalendar,
		hqgohttpmime.IconFormat,
		hqgohttpmime.JavaArchive,
		hqgohttpmime.JavaScript,
		hqgohttpmime.JPEG,
		hqgohttpmime.JSON,
		hqgohttpmime.JSONLD,
		hqgohttpmime.MIDI,
		hqgohttpmime.MSEmbeddedOpenTypeFonts,
		hqgohttpmime.MSExcel,
		hqgohttpmime.MSExcelOpenXML,
		hqgohttpmime.MSPowerPoint,
		hqgohttpmime.MSPowerPointOpenXML,
		hqgohttpmime.MSVisio,
		hqgohttpmime.MSWord,
		hqgohttpmime.MSWordOpenXML,
		hqgohttpmime.MP3Audio,
		hqgohttpmime.MP4Video,
		hqgohttpmime.MPEGTransportStream,
		hqgohttpmime.MPEGVideo,
		hqgohttpmime.MultipartFormData,
		hqgohttpmime.OGG,
		hqgohttpmime.OGGAudio,
		hqgohttpmime.OGGVideo,
		hqgohttpmime.OpusAudio,
		hqgohttpmime.OpenDocumentPresentation,
		hqgohttpmime.OpenDocumentSpreadsheet,
		hqgohttpmime.OpenDocumentText,
		hqgohttpmime.OpenTypeFont,
		hqgohttpmime.PDF,
		hqgohttpmime.PHP,
		hqgohttpmime.PNG,
		hqgohttpmime.RARArchive,
		hqgohttpmime.RichTextFormat,
		hqgohttpmime.SevenZipArchive,
		hqgohttpmime.SVG,
		hqgohttpmime.TARArchive,
		hqgohttpmime.Text,
		hqgohttpmime.ThreeG2AudioVideo,
		hqgohttpmime.ThreeGPAudioVideo,
		hqgohttpmime.TIFF,
		hqgohttpmime.TrueTypeFont,
		hqgohttpmime.WAVAudio,
		hqgohttpmime.WEBMAudio,
		hqgohttpmime.WEBMVideo,
		hqgohttpmime.WEBPImage,
		hqgohttpmime.WOFF,
		hqgohttpmime.WOFF2,
		hqgohttpmime.XML,
		hqgohttpmime.XHTML,
		hqgohttpmime.XUL,
		hqgohttpmime.ZIPArchive,
	}

	seen := make(map[hqgohttpmime.MIME]struct{}, len(values))

	for _, value := range values {
		assert.NotEmpty(t, value.String())

		_, duplicate := seen[value]
		assert.False(t, duplicate, "duplicate MIME value %q", value)

		seen[value] = struct{}{}
	}
}
