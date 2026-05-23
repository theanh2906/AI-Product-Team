package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/agent"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
	"google.golang.org/genai"
)

func main() {
	// 1. Lấy các biến môi trường
	githubToken := os.Getenv("GITHUB_TOKEN")
	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	orchestratorRepo := os.Getenv("GITHUB_REPOSITORY")
	owner := os.Getenv("GITHUB_REPOSITORY_OWNER")
	issueNumStr := os.Getenv("ISSUE_NUMBER")
	issueBody := os.Getenv("ISSUE_BODY")
	issueTitle := os.Getenv("ISSUE_TITLE")

	// Tên của Repo Sản Phẩm được truyền từ Workflow
	productRepoName := os.Getenv("PRODUCT_REPO_NAME")

	ctx := context.Background()

	// Khởi tạo GitHub Client
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: githubToken})
	tc := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(tc)

	var orchestratorRealName string
	if parts := strings.Split(orchestratorRepo, "/"); len(parts) == 2 {
		orchestratorRealName = parts[1]
	} else {
		orchestratorRealName = orchestratorRepo
	}
	issueNumber, _ := strconv.Atoi(issueNumStr)

	// 2. Gọi "Bộ Não" Gemini (Giữ nguyên phần cấu hình Schema ở lượt trước)
	fmt.Println("🧠 [Team Lead Agent]: Đang gửi Context qua Gemini để phân tích...")
	aiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiAPIKey})
	if err != nil {
		fmt.Printf("❌ Không thể tạo Gemini Client: %v\n", err)
		os.Exit(1)
	}

	systemInstruction := `Bạn là một AI Team Lead kiêm Business Analyst xuất sắc. Nhiệm vụ của bạn là bẻ nhỏ Issue thành các Task kỹ thuật chi tiết dưới dạng JSON.`
	prompt := fmt.Sprintf("Hãy phân tích yêu cầu sau đây từ PM Ben:\nTiêu đề: %s\nNội dung chi tiết:\n%s", issueTitle, issueBody)

	resp, err := aiClient.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(systemInstruction),
			},
		},
		ResponseMIMEType:  "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"analysis": {Type: genai.TypeString},
				"tasks": {
					Type: genai.TypeArray,
					Items: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"title":       {Type: genai.TypeString},
							"description": {Type: genai.TypeString},
							"assignee":    {Type: genai.TypeString},
							"branch_name": {Type: genai.TypeString},
							"depends_on":  {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
						},
					},
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("❌ Gemini gọi lỗi: %v\n", err)
		os.Exit(1)
	}

	var aiResult agent.AIResponse
	rawJSON := resp.Text()
	json.Unmarshal([]byte(rawJSON), &aiResult)

	// 3. TẠO BRANCH TỰ ĐỘNG BÊN REPO SẢN PHẨM 🌿
	fmt.Printf("🌿 [Team Lead Agent]: Bắt đầu khởi tạo các Branch bên Repo: %s...\n", productRepoName)

	// 3.1 Thường mặc định gốc sẽ là branch 'main'
	defaultBranch := "main"

	// 3.2 Lấy thông tin (Mã SHA) của branch main hiện tại bên Repo Sản Phẩm để làm gốc
	ref, _, err := ghClient.Repositories.GetBranch(ctx, owner, productRepoName, defaultBranch, 0)
	if err != nil {
		fmt.Printf("❌ Không lấy được thông tin branch %s của Repo sản phẩm: %v. Hãy chắc chắn tên repo đúng và repo không trống.\n", defaultBranch, err)
		os.Exit(1)
	}
	baseSHA := ref.GetCommit().GetSHA()
	fmt.Printf("🎯 Mã SHA gốc của branch %s là: %s\n", defaultBranch, baseSHA)

	// 3.3 Duyệt mảng task để tạo từng Branch tương ứng
	createdBranchesReport := "\n### 🌿 Trạng thái khởi tạo Git Branches bên Repo Sản Phẩm:\n"

	for _, task := range aiResult.Tasks {
		// Chuẩn hóa tên branch (xóa khoảng trắng nếu AI lỡ tạo sai quy tắc)
		cleanBranchName := strings.ReplaceAll(task.BranchName, " ", "-")

		// Định nghĩa cấu trúc Ref trên GitHub (bắt buộc phải có tiền tố "refs/heads/")
		refString := fmt.Sprintf("refs/heads/%s", cleanBranchName)

		newRef := &github.Reference{
			Ref:    github.String(refString),
			Object: &github.GitObject{SHA: github.String(baseSHA)},
		}

		// Gọi API GitHub tạo branch mới
		_, _, err := ghClient.Git.CreateRef(ctx, owner, productRepoName, newRef)
		if err != nil {
			// Nếu branch đã tồn tại từ trước (lần chạy cũ), chúng ta ghi nhận lại chứ không làm crash hệ thống
			if strings.Contains(err.Error(), "already exists") {
				createdBranchesReport += fmt.Sprintf("- `🌿 %s`: Trùng tên (Đã tồn tại từ trước) ⚠️\n", cleanBranchName)
			} else {
				createdBranchesReport += fmt.Sprintf("- `🌿 %s`: Tạo thất bại (Lỗi: %v) ❌\n", cleanBranchName, err)
			}
		} else {
			createdBranchesReport += fmt.Sprintf("- `🌿 %s`: Khởi tạo thành công cho **[%s]** ✅\n", cleanBranchName, task.Assignee)
		}
	}

	// 4. Tổng hợp báo cáo Markdown cuối cùng gửi cho ông Ben
	markdownReport := fmt.Sprintf("🤖 **[Team Lead & BA Agent Report]**\n\n### 📑 Phân tích tổng quan:\n%s\n\n### 📋 Danh sách Task phân rã:\n", aiResult.Analysis)
	for i, task := range aiResult.Tasks {
		markdownReport += fmt.Sprintf("%d. **[%s]** %s\n", i+1, task.Assignee, task.Title)
		markdownReport += fmt.Sprintf("   - *Mô tả:* %s\n", task.Description)
		markdownReport += fmt.Sprintf("   - *Branch:* `%s`\n", task.BranchName)
		markdownReport += "\n"
	}

	// Nối thêm phần báo cáo trạng thái tạo branch vào đuôi comment
	markdownReport += createdBranchesReport

	comment := &github.IssueComment{Body: github.String(markdownReport)}
	ghClient.Issues.CreateComment(ctx, owner, orchestratorRealName, issueNumber, comment)

	fmt.Println("🎉 Hoàn tất toàn bộ chu trình khởi tạo của Team Lead!")
}
