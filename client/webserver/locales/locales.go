package locales

var (
	Locales map[string]map[string]string
)

func init() {
	Locales = map[string]map[string]string{
		"en-US": EnUS,
		"pt-BR": PtBr,
		"zh-CN": ZhCN,
		"pl-PL": PlPL,
	}
}
