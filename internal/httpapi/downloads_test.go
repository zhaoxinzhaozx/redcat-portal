package httpapi

import "testing"

func TestParseDownloadsKeepsHTTPAddresses(t *testing.T) {
	got := ParseDownloads("5|Windows 下载|Windows 版工具|https://example.com/a.msi;;6|Windows 下载|Windows 版程序|https://example.com/b.exe")
	if len(got) != 2 || got[0].Step != 5 || got[1].URL != "https://example.com/b.exe" {
		t.Fatalf("%+v", got)
	}
}

func TestParseDownloadsDropsUnusableValues(t *testing.T) {
	got := ParseDownloads("5|Windows 下载|工具|javascript:alert(1);;bad;;9|按钮|名称|https://example.com/a")
	if len(got) != 1 || got[0].Step != 9 {
		t.Fatalf("%+v", got)
	}
}
