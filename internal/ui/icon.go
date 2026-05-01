// Package ui — templ에서 import하는 UI 헬퍼.
// 현재는 Lucide SVG 아이콘 embed.
package ui

import (
	"embed"
	"fmt"
	"html/template"
	"strings"
)

//go:embed icons/*.svg
var iconFS embed.FS

// IconSVG — Lucide SVG 문자열을 주어진 크기 + stroke-width 1.5 로 반환.
// 존재하지 않는 이름이면 placeholder span 반환 (dev에서 실수 발견 쉽게).
func IconSVG(name string, size int) template.HTML {
	b, err := iconFS.ReadFile("icons/" + name + ".svg")
	if err != nil {
		return template.HTML(fmt.Sprintf(
			`<span class="text-danger text-xs">[icon:%s]</span>`, name,
		))
	}
	svg := string(b)
	// Lucide SVG는 width/height/stroke-width 속성이 없거나 기본값(24/24/2)이 박혀 있음.
	// 우리는 size + stroke-width=1.5 로 덮어써야 함.
	// 간단하게 <svg ... width= height= stroke-width= 존재 여부와 무관하게
	// <svg 바로 뒤에 우리 속성을 삽입 (나중 속성이 이김).
	svg = strings.Replace(svg, "<svg",
		fmt.Sprintf(`<svg width="%d" height="%d" stroke-width="1.5"`, size, size), 1)
	return template.HTML(svg)
}
