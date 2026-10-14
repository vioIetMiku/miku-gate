package main

import (
	"strconv"
	"strings"
)

// 国家码转成"国旗 中文"，界面和节点别名都用这个形式。
//
// 国旗不查表：两字母国家码逐字符加上 regional indicator 的偏移就是对应的旗，
// 所以任何合法的 ISO 3166-1 alpha-2 都能出旗，不用跟着维护。
// 中文名就没有这种规律了，只能列表；表里没有的回退到清单给的英文名。

// countryCN 是国家码到中文名的对照。VPN Gate 的志愿者节点集中在东亚，
// 但清单里零星出现过几十个国家，所以列得宽一些。
var countryCN = map[string]string{
	// 东亚 / 东南亚
	"JP": "日本", "KR": "韩国", "KP": "朝鲜", "CN": "中国", "HK": "香港",
	"TW": "台湾", "MO": "澳门", "MN": "蒙古", "SG": "新加坡", "MY": "马来西亚",
	"TH": "泰国", "VN": "越南", "PH": "菲律宾", "ID": "印尼", "BN": "文莱",
	"KH": "柬埔寨", "LA": "老挝", "MM": "缅甸", "TL": "东帝汶",
	// 南亚 / 中亚
	"IN": "印度", "PK": "巴基斯坦", "BD": "孟加拉", "LK": "斯里兰卡",
	"NP": "尼泊尔", "BT": "不丹", "MV": "马尔代夫", "AF": "阿富汗",
	"KZ": "哈萨克斯坦", "UZ": "乌兹别克斯坦", "KG": "吉尔吉斯斯坦",
	"TJ": "塔吉克斯坦", "TM": "土库曼斯坦",
	// 西亚 / 中东
	"TR": "土耳其", "IL": "以色列", "AE": "阿联酋", "SA": "沙特",
	"QA": "卡塔尔", "KW": "科威特", "BH": "巴林", "OM": "阿曼",
	"JO": "约旦", "LB": "黎巴嫩", "SY": "叙利亚", "IQ": "伊拉克",
	"IR": "伊朗", "YE": "也门", "GE": "格鲁吉亚", "AM": "亚美尼亚",
	"AZ": "阿塞拜疆", "CY": "塞浦路斯",
	// 欧洲
	"RU": "俄罗斯", "UA": "乌克兰", "BY": "白俄罗斯", "MD": "摩尔多瓦",
	"PL": "波兰", "CZ": "捷克", "SK": "斯洛伐克", "HU": "匈牙利",
	"RO": "罗马尼亚", "BG": "保加利亚", "RS": "塞尔维亚", "HR": "克罗地亚",
	"SI": "斯洛文尼亚", "BA": "波黑", "ME": "黑山", "MK": "北马其顿",
	"AL": "阿尔巴尼亚", "GR": "希腊", "IT": "意大利", "ES": "西班牙",
	"PT": "葡萄牙", "FR": "法国", "DE": "德国", "AT": "奥地利",
	"CH": "瑞士", "NL": "荷兰", "BE": "比利时", "LU": "卢森堡",
	"GB": "英国", "IE": "爱尔兰", "DK": "丹麦", "SE": "瑞典",
	"NO": "挪威", "FI": "芬兰", "IS": "冰岛", "EE": "爱沙尼亚",
	"LV": "拉脱维亚", "LT": "立陶宛", "MT": "马耳他", "MC": "摩纳哥",
	"AD": "安道尔", "SM": "圣马力诺", "LI": "列支敦士登",
	// 美洲
	"US": "美国", "CA": "加拿大", "MX": "墨西哥", "BR": "巴西",
	"AR": "阿根廷", "CL": "智利", "CO": "哥伦比亚", "PE": "秘鲁",
	"VE": "委内瑞拉", "EC": "厄瓜多尔", "BO": "玻利维亚", "PY": "巴拉圭",
	"UY": "乌拉圭", "CR": "哥斯达黎加", "PA": "巴拿马", "GT": "危地马拉",
	"HN": "洪都拉斯", "SV": "萨尔瓦多", "NI": "尼加拉瓜", "CU": "古巴",
	"DO": "多米尼加", "PR": "波多黎各", "JM": "牙买加", "TT": "特立尼达",
	"BS": "巴哈马", "BZ": "伯利兹",
	// 非洲
	"EG": "埃及", "MA": "摩洛哥", "DZ": "阿尔及利亚", "TN": "突尼斯",
	"LY": "利比亚", "SD": "苏丹", "ET": "埃塞俄比亚", "KE": "肯尼亚",
	"TZ": "坦桑尼亚", "UG": "乌干达", "RW": "卢旺达", "NG": "尼日利亚",
	"GH": "加纳", "CI": "科特迪瓦", "SN": "塞内加尔", "CM": "喀麦隆",
	"CD": "刚果金", "CG": "刚果布", "AO": "安哥拉", "MZ": "莫桑比克",
	"ZM": "赞比亚", "ZW": "津巴布韦", "BW": "博茨瓦纳", "NA": "纳米比亚",
	"ZA": "南非", "MU": "毛里求斯", "MG": "马达加斯加",
	// 大洋洲
	"AU": "澳大利亚", "NZ": "新西兰", "FJ": "斐济", "PG": "巴新",
	"NC": "新喀里多尼亚", "GU": "关岛",
}

// flagOf 把两字母国家码变成国旗 emoji。码不合法时返回空串。
func flagOf(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		return ""
	}
	out := make([]rune, 0, 2)
	for i := 0; i < 2; i++ {
		c := code[i]
		if c < 'A' || c > 'Z' {
			return ""
		}
		// 'A' 对应 U+1F1E6，往后顺排
		out = append(out, rune(c-'A')+0x1F1E6)
	}
	return string(out)
}

// countryLabel 拼出"国旗 中文"，例如 "🇯🇵 日本"。
//
// fallback 是清单里的英文国名，中文表没收录时用它；连国家码都没有就只剩它。
func countryLabel(code, fallback string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	name := countryCN[code]
	if name == "" {
		name = strings.TrimSpace(fallback)
	}
	if name == "" {
		name = code
	}
	flag := flagOf(code)
	if flag == "" {
		return name
	}
	if name == "" {
		return flag
	}
	return flag + " " + name
}

// nodeLabel 是 countryLabel 的取节点信息版本。
func nodeLabel(n Node) string { return countryLabel(n.CountryCode, n.Country) }

// isGeneratedLabel 判断一个备注是不是 fanout 自己起的名字。
//
// 判据是开头那个国旗 emoji。用户手工起的备注极少这么开头，
// 所以这条足够把"自动名"和"我自己改的名"分开——换节点时只改前者，
// 用户改过的名字不能被悄悄冲掉。
func isGeneratedLabel(remark string) bool {
	r := []rune(strings.TrimSpace(remark))
	return len(r) > 0 && r[0] >= 0x1F1E6 && r[0] <= 0x1F1FF
}

// uniqueRemark 撞名时加序号。
//
// 同一条出口挂两个节点时别名会一样，而客户端（尤其 mihomo）要求名字唯一，
// 重了会直接丢掉后面那个。
func uniqueRemark(want string, taken map[string]bool) string {
	if want == "" || !taken[want] {
		return want
	}
	for i := 2; ; i++ {
		cand := want + " " + strconv.Itoa(i)
		if !taken[cand] {
			return cand
		}
	}
}
