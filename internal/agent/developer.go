package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v60/github"
	"google.golang.org/genai"
)

// FileChange đại diện cho một thay đổi file (tạo mới hoặc chỉnh sửa) do AI quyết định
type FileChange struct {
	Path    string `json:"path"`    // Đường dẫn tương đối từ gốc dự án, ví dụ "src/main.go"
	Content string `json:"content"` // Nội dung toàn bộ file mới/đã chỉnh sửa
}

// DeveloperResponse là cấu trúc phản hồi từ Gemini chứa giải thích và các thay đổi file
type DeveloperResponse struct {
	Explanation string       `json:"explanation"`
	Changes     []FileChange `json:"changes"`
}

// Developer đại diện cho AI Developer Agent
type Developer struct {
	Name string
}

// NewDeveloper khởi tạo Developer Agent mới
func NewDeveloper() *Developer {
	return &Developer{
		Name: "Developer-Agent",
	}
}

// DevelopTask thực hiện phân tích task, tự động viết code, tạo branch, commit, push và mở PR
func (d *Developer) DevelopTask(ctx context.Context, ghClient *github.Client, geminiAPIKey string, owner, repo string, issueNumber int, taskTitle string, taskDescription string, productRepoDir string, branchName string) (string, error) {
	fmt.Printf("💻 [%s]: Đang nghiên cứu task #%d: '%s'...\n", d.Name, issueNumber, taskTitle)

	// 1. Gọi Gemini SDK để sinh mã nguồn
	aiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiAPIKey})
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %w", err)
	}

	systemInstruction := `Bạn là một Senior Fullstack Engineer có kỹ năng viết mã nguồn tuyệt vời, sạch sẽ và tối ưu.
Nhiệm vụ của bạn là đọc yêu cầu phát triển hoặc mô tả lỗi (bug report), thiết kế các tệp mã nguồn cần thay đổi, và trả về toàn bộ danh sách các tệp cùng nội dung tương ứng của chúng dưới định dạng JSON theo schema được cung cấp.
Không sử dụng placeholders hay viết mã lấp lửng; mã nguồn phải chạy được luôn và hoàn chỉnh.`

	prompt := fmt.Sprintf("Hãy giải quyết task sau:\nTiêu đề: %s\nYêu cầu chi tiết:\n%s\n\nTrả về danh sách thay đổi tệp mã nguồn tương ứng.", taskTitle, taskDescription)

	resp, err := aiClient.Models.GenerateContent(ctx, "gemini-3-flash-review", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(systemInstruction),
			},
		},
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"explanation": {Type: genai.TypeString},
				"changes": {
					Type: genai.TypeArray,
					Items: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path":    {Type: genai.TypeString},
							"content": {Type: genai.TypeString},
						},
						Required: []string{"path", "content"},
					},
				},
			},
			Required: []string{"explanation", "changes"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate code from Gemini: %w", err)
	}

	var devResult DeveloperResponse
	if err := json.Unmarshal([]byte(resp.Text()), &devResult); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w, raw response: %s", err, resp.Text())
	}

	fmt.Printf("📋 [%s]: Giải thích giải pháp: %s\n", d.Name, devResult.Explanation)
	fmt.Printf("🛠️ [%s]: Nhận thấy %d tệp cần ghi/sửa đổi. Bắt đầu xử lý Git branch: %s...\n", d.Name, len(devResult.Changes), branchName)

	// 2. Thao tác Git: Tạo và checkout sang branch mới
	if err := runGitCommand(productRepoDir, "checkout", "main"); err != nil {
		// Thử checkout master nếu main không tồn tại
		_ = runGitCommand(productRepoDir, "checkout", "master")
	}
	// Xóa branch cũ nếu có từ trước để tạo mới tinh
	_ = runGitCommand(productRepoDir, "branch", "-D", branchName)
	if err := runGitCommand(productRepoDir, "checkout", "-b", branchName); err != nil {
		return "", fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	// 3. Ghi các file đã sinh ra đĩa
	for _, change := range devResult.Changes {
		// Tránh ghi ra ngoài thư mục Repo Product (bảo mật)
		cleanPath := filepath.Clean(change.Path)
		if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			fmt.Printf("⚠️ Warning: Bỏ qua tệp ngoài phạm vi: %s\n", change.Path)
			continue
		}

		filePath := filepath.Join(productRepoDir, cleanPath)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory for file %s: %w", filePath, err)
		}

		if err := os.WriteFile(filePath, []byte(change.Content), 0644); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fmt.Printf("💾 [%s]: Ghi thành công tệp: %s\n", d.Name, change.Path)
	}

	// 4. Commit & Push code
	if err := runGitCommand(productRepoDir, "add", "."); err != nil {
		return "", fmt.Errorf("failed to git add: %w", err)
	}
	commitMsg := fmt.Sprintf("feat: implement task #%d - %s", issueNumber, taskTitle)
	if err := runGitCommand(productRepoDir, "commit", "-m", commitMsg); err != nil {
		// Có thể không có thay đổi (nothing to commit)
		fmt.Printf("ℹ️ Không có thay đổi nào để commit hoặc git commit lỗi nhẹ: %v\n", err)
	}

	// Push branch lên remote (sử dụng -f để ghi đè nếu chạy lại)
	if err := runGitCommand(productRepoDir, "push", "origin", branchName, "-f"); err != nil {
		return "", fmt.Errorf("failed to git push branch %s: %w", branchName, err)
	}
	fmt.Printf("🚀 [%s]: Đã push code thành công lên branch %s!\n", d.Name, branchName)

	// 5. Tạo Pull Request trên GitHub
	prTitle := fmt.Sprintf("PR for Task #%d: %s", issueNumber, taskTitle)
	prBody := fmt.Sprintf("## 🤖 AI Developer Agent Pull Request\n\nTask liên quan: #%d\n\n### 📑 Giải thích thay đổi:\n%s", issueNumber, devResult.Explanation)
	
	newPR := &github.NewPullRequest{
		Title: github.String(prTitle),
		Head:  github.String(branchName),
		Base:  github.String("main"), // Thường base sẽ là main/master
		Body:  github.String(prBody),
	}

	pr, _, prErr := ghClient.PullRequests.Create(ctx, owner, repo, newPR)
	var prLink string
	if prErr != nil {
		if strings.Contains(prErr.Error(), "A pull request already exists") {
			// PR đã tồn tại từ trước, tìm PR hiện tại
			prs, _, _ := ghClient.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
				Head: owner + ":" + branchName,
			})
			if len(prs) > 0 {
				prLink = prs[0].GetHTMLURL()
				fmt.Printf("ℹ️ PR đã tồn tại từ trước: %s\n", prLink)
			} else {
				prLink = "*(Đã tồn tại Pull Request)*"
			}
		} else {
			fmt.Printf("⚠️ Cảnh báo: Không thể tạo PR: %v\n", prErr)
			prLink = "*(Không thể tự động tạo PR, bạn có thể tự tạo thủ công)*"
		}
	} else {
		prLink = pr.GetHTMLURL()
		fmt.Printf("✅ [%s]: Tạo thành công Pull Request: %s\n", d.Name, prLink)
	}

	summaryReport := fmt.Sprintf("🤖 **[AI Developer Agent Report]**\n\n- **Branch phát triển:** `%s`\n- **Giải thích:** %s\n- **Pull Request:** %s", branchName, devResult.Explanation, prLink)
	return summaryReport, nil
}

// runGitCommand thực thi một lệnh Git trong thư mục chỉ định
func runGitCommand(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v failed: %w, output: %s", args, err, string(output))
	}
	return nil
}
