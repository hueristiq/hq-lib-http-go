package httpclient_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestMIME_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mime     hqgohttp.MIME
		expected string
	}{
		{"HTML", hqgohttp.MIMEHTML, "text/html"},
		{"JSON", hqgohttp.MIMEJSON, "application/json"},
		{"JSONLD", hqgohttp.MIMEJSONLD, "application/ld+json"},
		{"XML", hqgohttp.MIMEXML, "application/xml"},
		{"Text", hqgohttp.MIMEText, "text/plain"},
		{"CSV", hqgohttp.MIMECSV, "text/csv"},
		{"PNG", hqgohttp.MIMEPNG, "image/png"},
		{"JPEG", hqgohttp.MIMEJPEG, "image/jpeg"},
		{"GIF", hqgohttp.MIMEGIF, "image/gif"},
		{"PDF", hqgohttp.MIMEPDF, "application/pdf"},
		{"ZIPArchive", hqgohttp.MIMEZIPArchive, "application/zip"},
		{"GZipCompressedArchive", hqgohttp.MIMEGZipCompressedArchive, "application/gzip"},
		{"MP4Video", hqgohttp.MIMEMP4Video, "video/mp4"},
		{"MP3Audio", hqgohttp.MIMEMP3Audio, "audio/mpeg"},
		{"WOFF2", hqgohttp.MIMEWOFF2, "font/woff2"},
		{"BinaryData", hqgohttp.MIMEBinaryData, "application/octet-stream"},
		{"MultipartFormData", hqgohttp.MIMEMultipartFormData, "multipart/form-data"},
		{"FormURLEncoded", hqgohttp.MIMEFormURLEncoded, "application/x-www-form-urlencoded"},
		{"EventStream", hqgohttp.MIMEEventStream, "text/event-stream"},
		{"JavaScript", hqgohttp.MIMEJavaScript, "text/javascript"},
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

	values := []hqgohttp.MIME{
		hqgohttp.MIMEAACAudio,
		hqgohttp.MIMEAbiWordDocument,
		hqgohttp.MIMEAmazonKindleEBook,
		hqgohttp.MIMEAppleInstallerPackage,
		hqgohttp.MIMEArchiveDocument,
		hqgohttp.MIMEAVIFImage,
		hqgohttp.MIMEAVIVideo,
		hqgohttp.MIMEBinaryData,
		hqgohttp.MIMEBitmapImage,
		hqgohttp.MIMEBourneShellScript,
		hqgohttp.MIMEBZip2Archive,
		hqgohttp.MIMEBZipArchive,
		hqgohttp.MIMECDAudio,
		hqgohttp.MIMECShellScript,
		hqgohttp.MIMECSS,
		hqgohttp.MIMECSV,
		hqgohttp.MIMEEPUB,
		hqgohttp.MIMEEventStream,
		hqgohttp.MIMEFormURLEncoded,
		hqgohttp.MIMEGZipCompressedArchive,
		hqgohttp.MIMEGIF,
		hqgohttp.MIMEHTML,
		hqgohttp.MIMEICalendar,
		hqgohttp.MIMEIconFormat,
		hqgohttp.MIMEJavaArchive,
		hqgohttp.MIMEJavaScript,
		hqgohttp.MIMEJPEG,
		hqgohttp.MIMEJSON,
		hqgohttp.MIMEJSONLD,
		hqgohttp.MIMEMIDI,
		hqgohttp.MIMEMSEmbeddedOpenTypeFonts,
		hqgohttp.MIMEMSExcel,
		hqgohttp.MIMEMSExcelOpenXML,
		hqgohttp.MIMEMSPowerPoint,
		hqgohttp.MIMEMSPowerPointOpenXML,
		hqgohttp.MIMEMSVisio,
		hqgohttp.MIMEMSWord,
		hqgohttp.MIMEMSWordOpenXML,
		hqgohttp.MIMEMP3Audio,
		hqgohttp.MIMEMP4Video,
		hqgohttp.MIMEMPEGTransportStream,
		hqgohttp.MIMEMPEGVideo,
		hqgohttp.MIMEMultipartFormData,
		hqgohttp.MIMEOGG,
		hqgohttp.MIMEOGGAudio,
		hqgohttp.MIMEOGGVideo,
		hqgohttp.MIMEOpusAudio,
		hqgohttp.MIMEOpenDocumentPresentation,
		hqgohttp.MIMEOpenDocumentSpreadsheet,
		hqgohttp.MIMEOpenDocumentText,
		hqgohttp.MIMEOpenTypeFont,
		hqgohttp.MIMEPDF,
		hqgohttp.MIMEPHP,
		hqgohttp.MIMEPNG,
		hqgohttp.MIMERARArchive,
		hqgohttp.MIMERichTextFormat,
		hqgohttp.MIMESevenZipArchive,
		hqgohttp.MIMESVG,
		hqgohttp.MIMETARArchive,
		hqgohttp.MIMEText,
		hqgohttp.MIMEThreeG2AudioVideo,
		hqgohttp.MIMEThreeGPAudioVideo,
		hqgohttp.MIMETIFF,
		hqgohttp.MIMETrueTypeFont,
		hqgohttp.MIMEWAVAudio,
		hqgohttp.MIMEWEBMAudio,
		hqgohttp.MIMEWEBMVideo,
		hqgohttp.MIMEWEBPImage,
		hqgohttp.MIMEWOFF,
		hqgohttp.MIMEWOFF2,
		hqgohttp.MIMEXML,
		hqgohttp.MIMEXHTML,
		hqgohttp.MIMEXUL,
		hqgohttp.MIMEZIPArchive,
	}

	seen := make(map[hqgohttp.MIME]struct{}, len(values))

	for _, value := range values {
		assert.NotEmpty(t, value.String())

		_, duplicate := seen[value]
		assert.False(t, duplicate, "duplicate MIME value %q", value)

		seen[value] = struct{}{}
	}
}
