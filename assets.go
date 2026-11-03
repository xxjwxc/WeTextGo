package wetext

import "embed"

// assets 内嵌 WeTextProcessing 预编译模型与默认规则，New() 默认直接使用，
// 调用方无需准备任何外部文件。
//
//	assets/<lang>_tagger_tn.wtfst      tagger FST（.wtfst 为紧凑格式，见 tools/wetext）
//	assets/<lang>_verbalizer_tn.wtfst  verbalizer FST
//	assets/orders.json                 各语种 token 字段顺序
//	assets/norm_rules.yaml             补充规则（词表/单位/货币/号码/后置正则）
//
//go:embed assets
var assets embed.FS

const (
	assetsDir       = "assets"
	defaultRulesTxt = assetsDir + "/norm_rules.yaml"
)
