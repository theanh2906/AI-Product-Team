package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/agent"
	ghWrapper "github.com/theanh2906/AI-Product-Team/internal/github"

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

	// Biến môi trường cho Kanban Board
	projectNumStr := os.Getenv("PROJECT_NUMBER")
	projectOwner := os.Getenv("PROJECT_OWNER")
	if projectOwner == "" {
		projectOwner = owner
	}

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
			projectID, err := wrapperClient.GetProjectV2ID(ctx, projectOwner, projectNum)
			if err != nil {
				fmt.Printf("❌ Không thể lấy Project ID cho Project #%d: %v\n", projectNum, err)
			} else {
				fmt.Printf("🎯 Tìm thấy Project ID: %s\n", projectID)

				// Lấy trường Status và Option "Todo"
				statusFieldID, todoOptionID, err := wrapperClient.GetProjectV2StatusField(ctx, projectID)
				if err != nil {
					fmt.Printf("⚠️ Cảnh báo: Không tìm thấy cột Status/Todo trên Board: %v. Các thẻ sẽ được xếp vào cột mặc định.\n", err)
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

					// 4.2 Thêm Issue vào Kanban Board và chuyển sang cột Todo
					_, err = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, todoOptionID, issueNodeID)
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
				wrapperClient := ghWrapper.NewClient(githubToken)
				projectID, projErr := wrapperClient.GetProjectV2ID(ctx, projectOwner, projectNum)
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
		wrapperClient := ghWrapper.NewClient(githubToken)
		projectID, err := wrapperClient.GetProjectV2ID(ctx, projectOwner, projectNum)
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
