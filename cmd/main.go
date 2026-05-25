package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/agent"
	ghWrapper "github.com/theanh2906/AI-Product-Team/internal/github"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
	"google.golang.org/genai"
)

// loadEnv loads environment variables from a local .env file if it exists.
func loadEnv() {
	file, err := os.Open(".env")
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Strip quotes if present
			if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
				val = val[1 : len(val)-1]
			} else if strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") {
				val = val[1 : len(val)-1]
			}
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func main() {
	// Load local environment variables from .env if present
	loadEnv()

	// 1. Lấy các biến môi trường
	githubToken := os.Getenv("GITHUB_TOKEN")
	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	orchestratorRepo := os.Getenv("GITHUB_REPOSITORY")
	owner := "theanh2906"
	issueNumStr := os.Getenv("ISSUE_NUMBER")
	issueBody := os.Getenv("ISSUE_BODY")
	issueTitle := os.Getenv("ISSUE_TITLE")

	// Tên của Repo Sản Phẩm được truyền từ Workflow
	productRepoName := os.Getenv("PRODUCT_REPO_NAME")

	// Biến môi trường cho Kanban Board
	projectNumStr := "3"
	projectOwner := "theanh2906"

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

	// 1.2 Kiểm tra chạy chế độ State-Machine hướng Kanban (PROJECT_ITEM_ID)
	projectItemID := os.Getenv("PROJECT_ITEM_ID")
	if projectItemID != "" {
		runKanbanStateMachineFlow(ctx, ghClient, githubToken, geminiAPIKey, projectItemID, projectNumStr, projectOwner)
		return
	}

	// 1.1 Kiểm tra chế độ chạy (Orchestrator Mode)
	orchestratorMode := strings.ToLower(os.Getenv("ORCHESTRATOR_MODE"))
	if orchestratorMode == "qa" {
		runQAAgentFlow(ctx, ghClient, githubToken, geminiAPIKey, owner, productRepoName, orchestratorRealName, issueNumber, issueTitle, projectNumStr, projectOwner)
		return
	}

	// 2. Gọi "Bộ Não" Gemini (Giữ nguyên phần cấu hình Schema ở lượt trước)
	fmt.Println("🧠 [Team Lead Agent]: Đang gửi Context qua Gemini để phân tích...")
	aiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiAPIKey})
	if err != nil {
		fmt.Printf("❌ Không thể tạo Gemini Client: %v\n", err)
		os.Exit(1)
	}

	systemInstruction := `Bạn là một AI Team Lead kiêm Business Analyst xuất sắc (PM). Nhiệm vụ của bạn là bẻ nhỏ Issue thành các Task kỹ thuật chi tiết dưới dạng JSON. Gán tất cả các task phát triển kỹ thuật (assignee) cho "Senior Fullstack Engineer".`
	prompt := fmt.Sprintf("Hãy phân tích yêu cầu sau đây từ PM Ben:\nTiêu đề: %s\nNội dung chi tiết:\n%s", issueTitle, issueBody)

	resp, err := aiClient.Models.GenerateContent(ctx, "gemini-3-flash-preview", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(systemInstruction),
			},
		},
		ResponseMIMEType: "application/json",
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

	// 4. TẠO ISSUE CON VÀ LIÊN KẾT VÀO KANBAN BOARD 📋
	createdIssuesReport := ""

	if projectNumStr != "" {
		projectNum, err := strconv.Atoi(projectNumStr)
		if err != nil {
			fmt.Printf("⚠️ Cảnh báo: PROJECT_NUMBER không hợp lệ (%s): %v\n", projectNumStr, err)
		} else {
			fmt.Printf("📋 [Kanban Integration]: Đang khởi tạo kết nối đến Project v2 (#%d) cho owner: %s...\n", projectNum, projectOwner)

			// Khởi tạo wrapper client của chúng ta
			wrapperClient := ghWrapper.NewClient(githubToken)

			// Lấy Project ID
			projectID := "3"
			var err error
			if err != nil {
				fmt.Printf("❌ Không thể lấy Project ID cho Project #%d: %v\n", projectNum, err)
			} else {
				fmt.Printf("🎯 Tìm thấy Project ID: %s\n", projectID)

				// Lấy trường Status và Option "Backlog"
				statusFieldID, options, err := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
				var backlogOptionID string
				if err == nil {
					if id, ok := options["backlog"]; ok {
						backlogOptionID = id
					} else if id, ok := options["todo"]; ok {
						backlogOptionID = id
					} else if len(options) > 0 {
						for _, id := range options {
							backlogOptionID = id
							break
						}
					}
				}
				if err != nil || backlogOptionID == "" {
					fmt.Printf("⚠️ Cảnh báo: Không tìm thấy cột Status/Backlog trên Board: %v. Các thẻ sẽ được xếp vào cột mặc định.\n", err)
				}

				createdIssuesReport = "\n### 📋 Trạng thái tạo Tasks & Bảng Kanban bên Repo Sản Phẩm:\n"

				for _, task := range aiResult.Tasks {
					// 4.1 Tạo Issue mới trên Repo Sản Phẩm
					issueTitle := fmt.Sprintf("[%s] %s", task.Assignee, task.Title)
					issueBody := fmt.Sprintf("%s\n\n---\n*Task được phân công cho: %s*\n*Branch dự kiến: `%s`*", task.Description, task.Assignee, task.BranchName)

					issueReq := &github.IssueRequest{
						Title: github.String(issueTitle),
						Body:  github.String(issueBody),
					}

					createdIssue, _, err := ghClient.Issues.Create(ctx, owner, productRepoName, issueReq)
					if err != nil {
						createdIssuesReport += fmt.Sprintf("- **%s**: Tạo Issue thất bại (Lỗi: %v) ❌\n", task.Title, err)
						continue
					}

					issueNum := createdIssue.GetNumber()
					issueNodeID := createdIssue.GetNodeID()
					fmt.Printf("✅ Đã tạo Issue #%d cho task: %s\n", issueNum, task.Title)

					// 4.2 Thêm Issue vào Kanban Board và chuyển sang cột Backlog
					_, err = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, backlogOptionID, issueNodeID)
					if err != nil {
						createdIssuesReport += fmt.Sprintf("- **%s**: Đã tạo Issue [#%d](https://github.com/%s/%s/issues/%d) nhưng lỗi liên kết Kanban board (Lỗi: %v) ⚠️\n",
							task.Title, issueNum, owner, productRepoName, issueNum, err)
					} else {
						createdIssuesReport += fmt.Sprintf("- **%s**: Khởi tạo Issue [#%d](https://github.com/%s/%s/issues/%d) và thêm vào Kanban Board thành công! ✅\n",
							task.Title, issueNum, owner, productRepoName, issueNum)
					}
				}
			}
		}
	} else {
		fmt.Println("ℹ️ PROJECT_NUMBER không được cấu hình. Bỏ qua bước tạo Task con trên Kanban Board.")
	}

	// 5. Tổng hợp báo cáo Markdown cuối cùng gửi cho ông Ben
	markdownReport := fmt.Sprintf("🤖 **[Team Lead & BA Agent Report]**\n\n### 📑 Phân tích tổng quan:\n%s\n\n### 📋 Danh sách Task phân rã:\n", aiResult.Analysis)
	for i, task := range aiResult.Tasks {
		markdownReport += fmt.Sprintf("%d. **[%s]** %s\n", i+1, task.Assignee, task.Title)
		markdownReport += fmt.Sprintf("   - *Mô tả:* %s\n", task.Description)
		markdownReport += fmt.Sprintf("   - *Branch:* `%s`\n", task.BranchName)
		markdownReport += "\n"
	}

	// Nối thêm phần báo cáo trạng thái tạo branch vào đuôi comment
	markdownReport += createdBranchesReport

	// Nối thêm báo cáo tạo task trên Kanban
	if createdIssuesReport != "" {
		markdownReport += createdIssuesReport
	}

	comment := &github.IssueComment{Body: github.String(markdownReport)}
	ghClient.Issues.CreateComment(ctx, owner, orchestratorRealName, issueNumber, comment)

	fmt.Println("🎉 Hoàn tất toàn bộ chu trình khởi tạo của Team Lead!")
}

// runQAAgentFlow điều phối luồng chạy kiểm thử của QA Agent
func runQAAgentFlow(ctx context.Context, ghClient *github.Client, githubToken, geminiAPIKey, owner, productRepoName, orchestratorRealName string, issueNumber int, issueTitle string, projectNumStr, projectOwner string) {
	fmt.Println("🧪 [QA Agent]: Đang khởi chạy chế độ kiểm thử...")

	// 1. Lấy cấu hình lệnh test (Mặc định go test ./...)
	testCommand := os.Getenv("TEST_COMMAND")
	if testCommand == "" {
		testCommand = "go test ./..."
	}

	// 2. Chạy QA Agent để thực thi test suite
	qaAgent := agent.NewQAAgent()
	testLog, pass, err := qaAgent.RunTests(ctx, testCommand)
	if err != nil {
		fmt.Printf("❌ Lỗi khi chạy test: %v\n", err)
		os.Exit(1)
	}

	var reportBody string
	var targetStatus string // "done" hoặc "backlog"

	// 3. Xử lý kết quả test
	if pass {
		// Test Passed
		reportBody = fmt.Sprintf("🤖 **[AI QA Agent Report]**\n\n✅ **Kiểm thử THÀNH CÔNG! (QA Passed)**\n\n- **Lệnh chạy:** `%s`\n\n### 🖥️ Chi tiết log kiểm thử:\n```text\n%s\n```", testCommand, testLog)
		targetStatus = "done"
	} else {
		// Test Failed -> Gọi Gemini chẩn đoán lỗi
		diagnosis, diagErr := qaAgent.DiagnoseFailure(ctx, geminiAPIKey, testLog, issueTitle)
		if diagErr != nil {
			fmt.Printf("⚠️ Cảnh báo: Không thể gọi Gemini chẩn đoán lỗi: %v\n", diagErr)
			diagnosis = "*(Không thể lấy phân tích chẩn đoán lỗi tự động từ Gemini)*"
		}

		// 3.1 Tạo Issue báo lỗi mới (như mong muốn của User)
		bugTitle := fmt.Sprintf("[QA Failed] Bug found: %s", issueTitle)
		bugBody := fmt.Sprintf("## ❌ Phát hiện lỗi trong quá trình kiểm thử tự động\n\n**Task liên quan:** #%d\n**Lệnh kiểm thử:** `%s`\n\n### 📑 Phân tích lỗi từ AI QA:\n%s\n\n### 🖥️ Chi tiết log kiểm thử:\n```text\n%s\n```", issueNumber, testCommand, diagnosis, testLog)

		fmt.Printf("📋 [QA Agent]: Đang tạo Bug Issue trên Repo: %s...\n", productRepoName)
		bugReq := &github.IssueRequest{
			Title:  github.String(bugTitle),
			Body:   github.String(bugBody),
			Labels: &[]string{"bug", "qa-failed"},
		}

		createdBug, _, createBugErr := ghClient.Issues.Create(ctx, owner, productRepoName, bugReq)
		var bugIssueLink string
		if createBugErr != nil {
			fmt.Printf("❌ Không thể tạo Bug Issue trên GitHub: %v\n", createBugErr)
			bugIssueLink = "*(Lỗi khi tạo Bug Issue tự động)*"
		} else {
			bugNum := createdBug.GetNumber()
			bugNodeID := createdBug.GetNodeID()
			bugIssueLink = fmt.Sprintf("[#%d](https://github.com/%s/%s/issues/%d)", bugNum, owner, productRepoName, bugNum)
			fmt.Printf("✅ Đã tạo Bug Issue #%d thành công!\n", bugNum)

			// 3.2 Liên kết Bug Issue này vào Kanban Board (cột đầu tiên như Backlog/PM)
			if projectNumStr != "" {
				projectNum, _ := strconv.Atoi(projectNumStr)
				_ = projectNum
				wrapperClient := ghWrapper.NewClient(githubToken)
				projectID := "4"
				var projErr error
				if projErr == nil {
					statusFieldID, options, optErr := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
					if optErr == nil {
						// Tìm cột mặc định để đưa thẻ Bug vào
						var targetColID string
						if id, ok := options["backlog"]; ok {
							targetColID = id
						} else if id, ok := options["pm"]; ok {
							targetColID = id
						} else if id, ok := options["todo"]; ok {
							targetColID = id
						}

						if targetColID != "" {
							_, _ = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, targetColID, bugNodeID)
							fmt.Printf("🎯 Đã liên kết thẻ Bug vào bảng Kanban ở cột khởi đầu.\n")
						}
					}
				}
			}
		}

		reportBody = fmt.Sprintf("🤖 **[AI QA Agent Report]**\n\n❌ **Kiểm thử THẤT BẠI! (QA Failed)**\n\n- **Lệnh chạy:** `%s`\n- **Bug Issue được tạo:** %s\n\n### 📑 Phân tích lỗi từ AI:\n%s\n\n<details>\n<summary>Xem chi tiết log kiểm thử</summary>\n\n```text\n%s\n```\n\n</details>", testCommand, bugIssueLink, diagnosis, testLog)
		targetStatus = "backlog"
	}

	// 4. Comment báo cáo lên PR/Issue hiện tại
	comment := &github.IssueComment{Body: github.String(reportBody)}
	_, _, commentErr := ghClient.Issues.CreateComment(ctx, owner, orchestratorRealName, issueNumber, comment)
	if commentErr != nil {
		fmt.Printf("❌ Không thể bình luận báo cáo QA lên GitHub: %v\n", commentErr)
	}

	// 5. Cập nhật trạng thái Kanban card của Task/PR gốc (nếu có Kanban)
	if projectNumStr != "" {
		projectNum, _ := strconv.Atoi(projectNumStr)
		_ = projectNum
		wrapperClient := ghWrapper.NewClient(githubToken)
		projectID := "3"
		var err error
		if err == nil {
			statusFieldID, options, err := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
			if err == nil {
				// Tìm ID của cột đích tương ứng
				var targetColID string
				if id, ok := options[targetStatus]; ok {
					targetColID = id
				} else if targetStatus == "backlog" {
					if id, ok := options["pm"]; ok {
						targetColID = id
					} else if id, ok := options["todo"]; ok {
						targetColID = id
					}
				}

				if targetColID != "" {
					// Lấy Node ID của Issue/PR gốc để tìm Item ID tương ứng trên Kanban
					refIssue, _, err := ghClient.Issues.Get(ctx, owner, orchestratorRealName, issueNumber)
					if err == nil {
						issueNodeID := refIssue.GetNodeID()
						itemID, err := wrapperClient.GetProjectV2ItemIDByContentID(ctx, projectID, issueNodeID)
						if err == nil {
							// Di chuyển card sang cột tương ứng
							err = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, itemID, statusFieldID, targetColID)
							if err == nil {
								fmt.Printf("🎯 Đã chuyển trạng thái Task gốc sang: %s\n", targetStatus)
							} else {
								fmt.Printf("❌ Không thể cập nhật trạng thái Task trên Kanban: %v\n", err)
							}
						} else {
							fmt.Printf("⚠️ Cảnh báo: Không tìm thấy Kanban Item cho Task hiện tại: %v\n", err)
						}
					}
				}
			}
		}
	}

	fmt.Println("🎉 Hoàn tất chu trình kiểm thử của QA Agent!")
}

// runGitCommand thực thi lệnh git trong thư mục chỉ định
func runGitCommand(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v failed: %w, output: %s", args, err, string(output))
	}
	return nil
}

// sanitizeBranchName chuẩn hóa tên branch theo công thức: task-<issue_number>-<sanitized_title>
func sanitizeBranchName(title string) string {
	title = strings.ToLower(title)
	var sb strings.Builder
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			sb.WriteRune('-')
		}
	}
	res := sb.String()
	// Thay thế nhiều dấu gạch ngang liên tiếp bằng một dấu
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	return strings.Trim(res, "-")
}

// runKanbanStateMachineFlow điều hướng luồng chạy dựa trên trạng thái của thẻ Kanban
func runKanbanStateMachineFlow(ctx context.Context, ghClient *github.Client, githubToken, geminiAPIKey, projectItemID string, projectNumStr, projectOwner string) {
	fmt.Printf("🗂️ [Kanban Router]: Kích hoạt State-Machine cho thẻ ID: %s\n", projectItemID)

	if projectNumStr == "" {
		fmt.Println("❌ Lỗi: PROJECT_NUMBER không được cấu hình.")
		os.Exit(1)
	}
	projectNum, err := strconv.Atoi(projectNumStr)
	_ = projectNum
	if err != nil {
		fmt.Printf("❌ Lỗi: PROJECT_NUMBER không hợp lệ: %v\n", err)
		os.Exit(1)
	}

	wrapperClient := ghWrapper.NewClient(githubToken)

	// 1. Lấy thông tin chi tiết thẻ
	details, err := wrapperClient.GetProjectV2ItemDetails(ctx, projectItemID)
	if err != nil {
		fmt.Printf("❌ Lỗi khi lấy thông tin thẻ Kanban: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📊 [Kanban Router]: Thẻ thuộc repo %s/%s, Trạng thái cột hiện tại: '%s'\n", details.RepoOwner, details.RepoName, details.Status)

	// Lấy Project ID
	projectID := "3"
	err = nil
	if err != nil {
		fmt.Printf("❌ Lỗi khi lấy Project ID: %v\n", err)
		os.Exit(1)
	}

	// Lấy danh sách các cột trên board
	statusFieldID, options, err := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
	if err != nil {
		fmt.Printf("❌ Lỗi khi lấy Status Options: %v\n", err)
		os.Exit(1)
	}

	statusNormalized := strings.ToLower(details.Status)

	switch statusNormalized {
	case "backlog":
		// Chạy Developer Agent (Senior Fullstack Engineer)
		runDeveloperAgentOnKanban(ctx, ghClient, wrapperClient, geminiAPIKey, details, projectID, statusFieldID, options)
	default:
		fmt.Printf("ℹ️ [Kanban Router]: Cột '%s' không có hành động tự động. Bỏ qua.\n", details.Status)
	}
}

// runTeamLeadAgentOnKanban xử lý khi thẻ ở cột PM: phân rã thành tasks con và đưa vào Backlog
func runTeamLeadAgentOnKanban(ctx context.Context, ghClient *github.Client, wrapperClient *ghWrapper.Client, geminiAPIKey string, details *ghWrapper.ProjectItemDetails, projectID, statusFieldID string, options map[string]string) {
	fmt.Println("🧠 [Team Lead Agent]: Phân tích yêu cầu và bẻ nhỏ task...")

	aiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiAPIKey})
	if err != nil {
		fmt.Printf("❌ Không thể tạo Gemini Client: %v\n", err)
		os.Exit(1)
	}

	systemInstruction := `Bạn là một AI Team Lead kiêm Business Analyst xuất sắc (PM). Nhiệm vụ của bạn là bẻ nhỏ Issue thành các Task kỹ thuật chi tiết dưới dạng JSON. Gán tất cả các task phát triển kỹ thuật (assignee) cho "Senior Fullstack Engineer".`
	prompt := fmt.Sprintf("Hãy phân tích yêu cầu sau đây từ PM Ben:\nTiêu đề: %s\nNội dung chi tiết:\n%s", details.Title, details.Body)

	resp, err := aiClient.Models.GenerateContent(ctx, "gemini-3-flash-preview", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(systemInstruction),
			},
		},
		ResponseMIMEType: "application/json",
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
	json.Unmarshal([]byte(resp.Text()), &aiResult)

	// Lấy SHA của nhánh main để làm gốc tạo branch
	defaultBranch := "main"
	ref, _, err := ghClient.Repositories.GetBranch(ctx, details.RepoOwner, details.RepoName, defaultBranch, 0)
	var baseSHA string
	if err == nil {
		baseSHA = ref.GetCommit().GetSHA()
	}

	createdBranchesReport := "\n### 🌿 Trạng thái khởi tạo Git Branches bên Repo Sản Phẩm:\n"
	createdIssuesReport := "\n### 📋 Trạng thái tạo Tasks & Bảng Kanban bên Repo Sản Phẩm:\n"

	for _, task := range aiResult.Tasks {
		// Tạo Issue mới
		issueTitle := fmt.Sprintf("[%s] %s", task.Assignee, task.Title)
		issueReq := &github.IssueRequest{
			Title: github.String(issueTitle),
			Body:  github.String(task.Description),
		}

		createdIssue, _, err := ghClient.Issues.Create(ctx, details.RepoOwner, details.RepoName, issueReq)
		if err != nil {
			createdIssuesReport += fmt.Sprintf("- **%s**: Tạo Issue thất bại (Lỗi: %v) ❌\n", task.Title, err)
			continue
		}

		issueNum := createdIssue.GetNumber()
		issueNodeID := createdIssue.GetNodeID()

		// Sinh tên branch chuẩn hóa: task-<number>-<title>
		cleanBranchName := sanitizeBranchName(fmt.Sprintf("task-%d-%s", issueNum, task.Title))

		// Cập nhật lại mô tả Issue cho đầy đủ
		updatedBody := fmt.Sprintf("%s\n\n---\n*Task được phân công cho: %s*\n*Branch dự kiến: `%s`*", task.Description, task.Assignee, cleanBranchName)
		_, _, _ = ghClient.Issues.Edit(ctx, details.RepoOwner, details.RepoName, issueNum, &github.IssueRequest{
			Body: github.String(updatedBody),
		})

		// Tạo branch trên GitHub
		if baseSHA != "" {
			refString := fmt.Sprintf("refs/heads/%s", cleanBranchName)
			newRef := &github.Reference{
				Ref:    github.String(refString),
				Object: &github.GitObject{SHA: github.String(baseSHA)},
			}
			_, _, err = ghClient.Git.CreateRef(ctx, details.RepoOwner, details.RepoName, newRef)
			if err != nil {
				createdBranchesReport += fmt.Sprintf("- `🌿 %s`: Tạo thất bại hoặc đã tồn tại ⚠️\n", cleanBranchName)
			} else {
				createdBranchesReport += fmt.Sprintf("- `🌿 %s`: Khởi tạo thành công cho **[%s]** ✅\n", cleanBranchName, task.Assignee)
			}
		}

		// Đưa card con vừa tạo vào cột Backlog
		var backlogColID string
		if id, ok := options["backlog"]; ok {
			backlogColID = id
		} else if id, ok := options["pm"]; ok {
			backlogColID = id
		}

		_, err = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, backlogColID, issueNodeID)
		if err != nil {
			createdIssuesReport += fmt.Sprintf("- **%s**: Đã tạo Issue [#%d](https://github.com/%s/%s/issues/%d) nhưng lỗi liên kết Kanban ⚠️\n",
				task.Title, issueNum, details.RepoOwner, details.RepoName, issueNum, err)
		} else {
			createdIssuesReport += fmt.Sprintf("- **%s**: Khởi tạo Issue [#%d](https://github.com/%s/%s/issues/%d) và thêm vào cột Backlog thành công! ✅\n",
				task.Title, issueNum, details.RepoOwner, details.RepoName, issueNum)
		}
	}

	// Đóng gói báo cáo gửi lên comment của Issue gốc
	markdownReport := fmt.Sprintf("🤖 **[Team Lead & BA Agent Report]**\n\n### 📑 Phân tích tổng quan:\n%s\n\n### 📋 Danh sách Task phân rã:\n", aiResult.Analysis)
	for i, task := range aiResult.Tasks {
		markdownReport += fmt.Sprintf("%d. **[%s]** %s\n", i+1, task.Assignee, task.Title)
	}
	markdownReport += createdBranchesReport + createdIssuesReport

	comment := &github.IssueComment{Body: github.String(markdownReport)}
	_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, comment)

	// Chuyển thẻ PM gốc sang cột "In progress"
	if inProgressColID, ok := options["in progress"]; ok {
		_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, inProgressColID)
	}

	fmt.Println("🎉 Hoàn tất chu trình phân rã task của Team Lead!")
}

// runDeveloperAgentOnKanban xử lý khi thẻ ở cột Backlog: tự động code và mở PR, chuyển sang In QA
func runDeveloperAgentOnKanban(ctx context.Context, ghClient *github.Client, wrapperClient *ghWrapper.Client, geminiAPIKey string, details *ghWrapper.ProjectItemDetails, projectID, statusFieldID string, options map[string]string) {
	fmt.Println("💻 [Developer Agent]: Nhận task và bắt đầu phát triển mã nguồn...")

	// 1. Xác định thư mục chạy code của Product
	productRepoDir := os.Getenv("TEST_DIR")
	if productRepoDir == "" {
		productRepoDir = "."
	}

	// 2. Chuyển thẻ Kanban sang "In progress" trước để thông báo đang làm việc
	if inProgressColID, ok := options["in progress"]; ok {
		_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, inProgressColID)
	}

	// Sinh tên branch chuẩn hóa đồng bộ với Kanban Item: task-<number>-<title>
	branchName := sanitizeBranchName(fmt.Sprintf("task-%d-%s", details.Number, details.Title))

	// 3. Khởi chạy AI Developer để lập trình
	devAgent := agent.NewDeveloper()
	summaryReport, err := devAgent.DevelopTask(ctx, ghClient, geminiAPIKey, details.RepoOwner, details.RepoName, details.Number, details.Title, details.Body, productRepoDir, branchName)

	if err != nil {
		fmt.Printf("❌ AI Developer lập trình thất bại: %v\n", err)
		// Trả card về cột Backlog
		if backlogColID, ok := options["backlog"]; ok {
			_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, backlogColID)
		}
		// Comment lỗi lên Issue
		errorComment := fmt.Sprintf("🤖 **[AI Developer Agent Report]**\n\n❌ **Quá trình sinh mã nguồn thất bại!**\n\n*Chi tiết lỗi:* `%v`", err)
		_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, &github.IssueComment{Body: github.String(errorComment)})
		return
	}

	// 4. Bình luận báo cáo lên Issue
	_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, &github.IssueComment{Body: github.String(summaryReport)})

	// 5. Chuyển thẻ sang cột "Done" để hoàn thành công việc
	if doneColID, ok := options["done"]; ok {
		err = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, doneColID)
		if err == nil {
			fmt.Println("🎯 Đã chuyển thẻ Kanban sang cột Done thành công!")
		}
	}
}

// runQAAgentOnKanban xử lý khi thẻ ở cột In QA: tự động test và cập nhật cột hoặc tạo bug
func runQAAgentOnKanban(ctx context.Context, ghClient *github.Client, wrapperClient *ghWrapper.Client, geminiAPIKey string, details *ghWrapper.ProjectItemDetails, projectID, statusFieldID string, options map[string]string) {
	fmt.Println("🧪 [QA Agent]: Bắt đầu chạy test suite kiểm định...")

	// 1. Xác định thư mục chạy code của Product
	productRepoDir := os.Getenv("TEST_DIR")
	if productRepoDir == "" {
		productRepoDir = "."
	}

	testCommand := os.Getenv("TEST_COMMAND")
	if testCommand == "" {
		testCommand = "go test ./..."
	}

	// Sinh tên branch tương ứng
	branchName := sanitizeBranchName(fmt.Sprintf("task-%d-%s", details.Number, details.Title))

	// Checkout sang branch tương ứng trước khi chạy test
	fmt.Printf("🌿 [QA Agent]: Checkout sang branch: %s\n", branchName)
	if err := runGitCommand(productRepoDir, "checkout", branchName); err != nil {
		fmt.Printf("⚠️ Cảnh báo: Không thể checkout sang branch %s: %v. Sẽ chạy test trên nhánh hiện tại.\n", branchName, err)
	}

	// 2. Chạy test
	qaAgent := agent.NewQAAgent()
	testLog, pass, err := qaAgent.RunTests(ctx, testCommand)
	if err != nil {
		fmt.Printf("❌ Lỗi hệ thống khi chạy test suite: %v\n", err)
		os.Exit(1)
	}

	var reportBody string
	var targetStatus string

	if pass {
		reportBody = fmt.Sprintf("🤖 **[AI QA Agent Report]**\n\n✅ **Kiểm thử THÀNH CÔNG! (QA Passed)**\n\n- **Lệnh chạy:** `%s`\n- **Branch kiểm thử:** `%s`\n\n### 🖥️ Chi tiết log kiểm thử:\n```text\n%s\n```", testCommand, branchName, testLog)
		targetStatus = "done"
	} else {
		// Gọi Gemini phân tích log lỗi
		diagnosis, diagErr := qaAgent.DiagnoseFailure(ctx, geminiAPIKey, testLog, details.Title)
		if diagErr != nil {
			diagnosis = "*(Không thể lấy phân tích chẩn đoán lỗi tự động từ Gemini)*"
		}

		// Tạo Bug Issue mới và xếp vào Kanban
		bugTitle := fmt.Sprintf("[QA Failed] Bug found: %s", details.Title)
		bugBody := fmt.Sprintf("## ❌ Phát hiện lỗi trong quá trình kiểm thử tự động\n\n**Task liên quan:** #%d\n**Lệnh kiểm thử:** `%s`\n**Branch kiểm thử:** `%s`\n\n### 📑 Phân tích lỗi từ AI QA:\n%s\n\n### 🖥️ Chi tiết log kiểm thử:\n```text\n%s\n```", details.Number, testCommand, branchName, diagnosis, testLog)

		bugReq := &github.IssueRequest{
			Title:  github.String(bugTitle),
			Body:   github.String(bugBody),
			Labels: &[]string{"bug", "qa-failed"},
		}

		createdBug, _, createBugErr := ghClient.Issues.Create(ctx, details.RepoOwner, details.RepoName, bugReq)
		var bugLink string
		if createBugErr == nil {
			bugNum := createdBug.GetNumber()
			bugNodeID := createdBug.GetNodeID()
			bugLink = fmt.Sprintf("[#%d](https://github.com/%s/%s/issues/%d)", bugNum, details.RepoOwner, details.RepoName, bugNum)

			// Đưa Bug Card vào cột Backlog
			if backlogColID, ok := options["backlog"]; ok {
				_, _ = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, backlogColID, bugNodeID)
			}
		} else {
			bugLink = "*(Lỗi khi tạo Bug Issue tự động)*"
		}

		reportBody = fmt.Sprintf("🤖 **[AI QA Agent Report]**\n\n❌ **Kiểm thử THẤT BẠI! (QA Failed)**\n\n- **Lệnh chạy:** `%s`\n- **Branch kiểm thử:** `%s`\n- **Bug Issue được tạo:** %s\n\n### 📑 Phân tích lỗi từ AI:\n%s\n\n<details>\n<summary>Xem chi tiết log kiểm thử</summary>\n\n```text\n%s\n```\n\n</details>", testCommand, branchName, bugLink, diagnosis, testLog)
		targetStatus = "backlog"
	}

	// Comment lên Issue
	_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, &github.IssueComment{Body: github.String(reportBody)})

	// Cập nhật trạng thái card hiện tại sang Done hoặc trả về Backlog
	var targetColID string
	if id, ok := options[targetStatus]; ok {
		targetColID = id
	} else if targetStatus == "backlog" {
		if id, ok := options["pm"]; ok {
			targetColID = id
		}
	}

	if targetColID != "" {
		_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, targetColID)
		fmt.Printf("🎯 Đã cập nhật trạng thái thẻ Kanban hiện tại sang: %s\n", targetStatus)
	}
}
