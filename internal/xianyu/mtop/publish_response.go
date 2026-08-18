package mtop

// findStringDeep 在嵌套发布响应中按优先键集提取首个非空字符串。
func findStringDeep(value any, keys ...string) string {
	// keySet 保存需要优先匹配的响应字段名。
	keySet := map[string]struct{}{}
	// key 是当前待登记的响应字段名。
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	// walk 递归遍历 map 和数组，优先读取当前 map 的目标字段。
	var walk func(any) string
	walk = func(current any) string {
		// typed 是当前递归节点断言后的响应容器类型。
		switch typed := current.(type) {
		case map[string]any:
			// key、nested 分别是当前响应对象的字段名和字段值。
			for key, nested := range typed {
				// wanted 表示当前字段是否属于调用方要求优先读取的键集。
				if _, wanted := keySet[key]; wanted {
					// text 是当前目标字段规范化后的非空文本。
					if text := mtopString(nested); text != "" {
						return text
					}
				}
			}
			// nested 是当前对象中待递归查询的非目标字段值。
			for _, nested := range typed {
				// text 是子树递归返回的首个非空文本。
				if text := walk(nested); text != "" {
					return text
				}
			}
		case []any:
			// nested 是当前数组中待递归查询的元素。
			for _, nested := range typed {
				// text 是子元素递归返回的首个非空文本。
				if text := walk(nested); text != "" {
					return text
				}
			}
		}
		return ""
	}
	return walk(value)
}
