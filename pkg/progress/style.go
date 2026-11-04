package progress

// Style 定义进度条的填充物
type Style struct {
	LeftBracket  string
	RightBracket string
	Filled       string
	Head         string
	Empty        string
}

// DefaultStyle 深浅阴影风格：▓▓▓▓█░░░░
func DefaultStyle() *Style {
	return &Style{
		LeftBracket:  "",
		RightBracket: "",
		Filled:       "▓",
		Head:         "█",
		Empty:        "░",
	}
}

// Colors 定义统计信息，错误信息，完成信息的着色
type Colors struct {
	Stats string
	Error string
	Done  string
}

// DefaultColors 默认着色
func DefaultColors() *Colors {
	return &Colors{
		Stats: colorStats,
		Error: colorError,
		Done:  colorDone,
	}
}
