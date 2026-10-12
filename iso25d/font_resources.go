package iso25d

import "github.com/MarkovWangRR/iso-topology/textmetrics"

// fontResources belongs to one composite render, never to a shape or a global
// cache. A nil set keeps public standalone renderers self-contained. Record a
// font only at the point where its text writer actually submits the resource;
// empty content and unsupported captions must not consume its first use.
type fontResources map[*textmetrics.BuiltinFont]struct{}

func (fonts fontResources) css(font *textmetrics.BuiltinFont) string {
	if font == nil {
		return ""
	}
	if fonts != nil {
		if _, seen := fonts[font]; seen {
			return ""
		}
		fonts[font] = struct{}{}
	}
	return font.CSS()
}
