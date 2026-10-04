package service

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// 密码策略常量。长度上下限对应 bcrypt 的实际约束：bcrypt 只处理前 72
// 字节，超出部分被静默丢弃（不会报错），所以字节数上限定在 72；字符数
// 另外限制在 32，避免用户把整段文章当密码（越长越难记，并无安全收益）。
const (
	MinPasswordChars = 8
	MaxPasswordChars = 32
	maxPasswordBytes = 72
)

// weakPasswords 是常见弱口令集合。键为转小写并去除首尾空白后的密码。
//
// 为什么需要它：原实现只校验长度 8-72，`12345678` / `password` / `qwer1234`
// 这类第一个就会被撞库词典试中的口令可以合法注册。列表刻意保持短小——
// 它是兜底防线，配合下面的字符类别要求，不是完整字典。
var weakPasswords = map[string]struct{}{
	"12345678": {}, "123456789": {}, "1234567890": {}, "1234567": {},
	"qwertyuiop": {}, "qwerty123": {}, "qwert1234": {}, "qwer1234": {},
	"asdfghjkl": {}, "asdasdasd": {}, "zxcvbnm123": {}, "1qaz2wsx": {},
	"password": {}, "password1": {}, "password123": {}, "passw0rd": {},
	"p@ssw0rd": {}, "iloveyou": {}, "admin123": {}, "administrator": {},
	"letmein1": {}, "welcome1": {}, "welcome123": {}, "abc12345": {},
	"abcd1234": {}, "aaa123456": {}, "a1234567": {}, "a12345678": {},
	"11111111": {}, "00000000": {}, "88888888": {}, "66666666": {},
	"123123123": {}, "147258369": {}, "65432100": {}, "0123456789": {},
	"qazwsxedc": {}, "1q2w3e4r": {}, "qweasdzxc": {}, "woaini1314": {},
	"woaini520": {}, "5201314a": {}, "zz123456": {}, "aa123456": {},
	"test1234": {}, "dev123456": {}, "root123456": {}, "inkstone1": {},
	"blog12345": {}, "google123": {}, "facebook1": {}, "myspace1": {},
	"sunshine1": {}, "princess1": {}, "football1": {}, "baseball1": {},
	"monkey123": {}, "dragon123": {}, "master123": {}, "shadow123": {},
	"superman1": {}, "batman123": {}, "trustno1": {}, "changeme1": {},
	"default1": {}, "guest1234": {}, "q1w2e3r4": {}, "1q2w3e4r5t": {},
	"a1b2c3d4": {}, "1a2b3c4d": {}, "31415926": {}, "521521521": {},
	"77585210": {}, "147147147": {}, "789456123": {}, "22222222": {},
}

// passwordCategories 返回密码覆盖的字符类别数：
// 小写字母、大写字母、数字、符号、CJK（中文及其他非 ASCII 文字）各自计一类。
// 只用来判断"至少两类"，因此实现保持简单——不需要判断具体是哪几类。
func passwordCategories(password string) int {
	var hasLower, hasUpper, hasDigit, hasSymbol, hasWide bool
	for _, r := range password {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		case r > 127:
			// CJK 与其它非 ASCII 文字单独成类：纯中文密码（如"春风秋月何时了"）
			// 只有一类，需搭配数字或符号；这也让中文密码不会被误判为"无字符"。
			hasWide = true
		default:
			hasSymbol = true
		}
	}
	n := 0
	for _, ok := range []bool{hasLower, hasUpper, hasDigit, hasSymbol, hasWide} {
		if ok {
			n++
		}
	}
	return n
}

// CheckPasswordStrength 校验新密码是否满足平台策略。
//
// 规则（三条硬性要求，全部给出可读的中文提示）：
//  1. 长度：8-32 个字符，且不超过 bcrypt 的 72 字节上限；
//  2. 字符类别：至少覆盖两类（字母/数字/符号/中文），挡住纯数字与纯小写词典口令；
//  3. 不得是常见弱口令，也不得等于用户自己的邮箱或用户名。
//
// password 传用户提交的原始密码；email / username 传该账号的既有信息
// （可为空，为空时跳过对应比对）。
func CheckPasswordStrength(password, email, username string) error {
	if len(password) > maxPasswordBytes {
		return NewValidationError(fmt.Sprintf("密码长度需在 %d-%d 个字符之间", MinPasswordChars, MaxPasswordChars))
	}
	runes := []rune(password)
	if len(runes) < MinPasswordChars || len(runes) > MaxPasswordChars {
		return NewValidationError(fmt.Sprintf("密码长度需在 %d-%d 个字符之间", MinPasswordChars, MaxPasswordChars))
	}
	if strings.TrimSpace(password) != password {
		return NewValidationError("密码首尾不能包含空格")
	}
	if passwordCategories(password) < 2 {
		return NewValidationError("密码需包含字母、数字或符号中的至少两类")
	}

	normalized := strings.ToLower(strings.TrimSpace(password))
	if _, weak := weakPasswords[normalized]; weak {
		return NewValidationError("该密码过于常见，请更换一个")
	}

	// 与自己账号信息相同：撞库时邮箱/用户名通常已知，这类口令等于没设。
	if account := strings.ToLower(strings.TrimSpace(username)); account != "" {
		if normalized == account {
			return NewValidationError("密码不能与用户名相同")
		}
		// 用户名较短时（如 "abc"）逐段比对意义不大，只比对完整值。
	}
	if account := strings.ToLower(strings.TrimSpace(email)); account != "" {
		if normalized == account {
			return NewValidationError("密码不能与邮箱相同")
		}
		if at := strings.Index(account, "@"); at > 0 {
			if normalized == account[:at] {
				return NewValidationError("密码不能与邮箱前缀相同")
			}
		}
	}
	return nil
}

// HashPassword / CheckPassword 是 bcrypt 的薄封装。
//
// 存在的原因：文章访问密码与用户密码用同一个哈希算法，但项目里此前
// 散落着多处直接调 bcrypt 的代码（auth_service / admin_service）。
// 加这两个函数是为了让"密码哈希"有单一入口——将来换成 argon2 之类
// 只改这里，不必逐个文件核对 cost 参数是否一致。
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword 比对明文与哈希。不匹配返回 false。
func CheckPassword(plain, hash string) bool {
	if hash == "" || plain == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
