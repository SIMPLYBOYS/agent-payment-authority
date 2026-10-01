# 第三方元件與授權

`go.sum` 是模組校驗資料，不是授權清單。本表列出**編進本專案程式**的第三方元件，依據是 2026-10-02 從本機模組快取讀到的各模組授權檔，以及 `modernc.org/sqlite` v1.60.1 隨附的 `LICENSE-3RD-PARTY.md`（第 1 節「Components linked into your program」）。

清單由下列指令產生；依賴有變動時重跑並更新本表：

```sh
go list -deps -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' ./... | sort -u
```

## Go 模組（10 個，全部經 `modernc.org/sqlite` 引入）

| 模組 | 版本 | 授權 | 授權檔 |
|---|---|---|---|
| `modernc.org/sqlite` | v1.60.1 | BSD-3-Clause | `LICENSE`；另有 `LICENSE-3RD-PARTY.md`、`LICENSE-SQLITE`、`LICENSE-SQLITE_VEC` |
| `modernc.org/libc` | v1.77.1 | BSD-3-Clause | `LICENSE`；另有 `LICENSE-3RD-PARTY.md` |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause | `LICENSE` |
| `modernc.org/memory` | v1.12.1 | BSD-3-Clause | `LICENSE`、`LICENSE-GO`、`LICENSE-MMAP-GO`；`LICENSE-LOGO` 只是標誌圖片的出處連結 |
| `golang.org/x/sys` | v0.48.0 | BSD-3-Clause | `LICENSE` |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause | `LICENSE` |
| `github.com/remyoudompheng/bigfft` | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause | `LICENSE` |
| `github.com/dustin/go-humanize` | v1.0.1 | MIT | `LICENSE` |
| `github.com/mattn/go-isatty` | v0.0.24 | MIT | `LICENSE` |
| `github.com/ncruces/go-strftime` | v1.0.0 | MIT | `LICENSE` |

`modernc.org/sqlite` 要求下游 `go.mod` 釘住與它相同的 `modernc.org/libc` 版本；本專案 `go.mod` 釘 v1.77.1（見檔內註解）。`go list -m all` 列出 26 個模組，其餘 16 個只用於解析模組圖，沒有編進程式。

## 轉譯或內嵌在上述模組裡的元件

| 元件 | 授權 | 位置 |
|---|---|---|
| SQLite 3.53.4 | public domain | 由 `modernc.org/ccgo` 轉譯成 Go，位於 `modernc.org/sqlite` 的 `lib/`（`LICENSE-SQLITE`） |
| musl libc | MIT | 經 `modernc.org/libc` |
| Go 標準函式庫的部分程式 | BSD-3-Clause | 經 `modernc.org/libc` |
| go-netdb | MIT | 經 `modernc.org/libc` |
| NixOS/nixpkgs 的部分內容 | MIT | 經 `modernc.org/libc` |

`sqlite-vec`（MIT）只在 import `modernc.org/sqlite/vec` 時才會連結，SQLite 的 `test_demovfs.c` 只在 import `modernc.org/sqlite/vfs` 時才會連結；本專案兩者都沒有 import。

以上依授權檔與上游清單整理，不構成法律意見。
