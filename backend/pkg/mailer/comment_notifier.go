package mailer

import (
	"fmt"
	"log"

	"github.com/shenwei/inkstone/backend/internal/service"
)

// CommentNotifier 实现 service.CommentNotifier：有新评论时给文章作者发邮件。
//
// 它存在的意义是把"评论通知"这件事从 CommentService 里剥出去——service 层
// 不该知道 SMTP，否则每次改发信逻辑都要动业务代码，且测试要用真 SMTP。
type CommentNotifier struct {
	inner   *Mailer
	baseURL string
}

func NewCommentNotifier(inner *Mailer, baseURL string) *CommentNotifier {
	return &CommentNotifier{inner: inner, baseURL: baseURL}
}

// NotifyAuthor 通知文章作者有新评论。
//
// 任何一步不满足（未配置 SMTP、作者没邮箱）都静默返回：通知是旁路功能，
// 不能让它影响评论本身能否发表。发送失败同样只记日志。
func (n *CommentNotifier) NotifyAuthor(articleTitle, articleSlug, authorEmail, commenter string) {
	if n == nil || n.inner == nil {
		return
	}
	if authorEmail == "" {
		return
	}
	link := fmt.Sprintf("%s/posts/%s", n.baseURL, articleSlug)
	who := "有人"
	if commenter != "" {
		who = commenter
	}
	subject := fmt.Sprintf("你的文章《%s》有新评论", articleTitle)
	body := fmt.Sprintf(
		"%s 评论了你的文章《%s》。\n\n查看：%s\n\n—— InkStone",
		who, articleTitle, link)
	if err := n.inner.Send(authorEmail, subject, body); err != nil {
		// 只记日志：见上方说明，通知失败不该让评论失败。
		log.Printf("[mail] 评论通知发送失败（%s）: %v", authorEmail, err)
	}
}

var _ service.CommentNotifier = (*CommentNotifier)(nil)
