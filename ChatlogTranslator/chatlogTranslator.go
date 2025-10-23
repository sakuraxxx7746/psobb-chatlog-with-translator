package main

// compile command
// go build -ldflags="-H=windowsgui" -o chatlogTranslator.exe

import (
    "bytes"
    "bufio"
    "fmt"
    "io"
    "io/ioutil"
    "net/http"
    "net/url"
    "strings"
    "regexp"
    "os"
    "encoding/json"
    "sort"
    "time"
    "path/filepath"
    "unsafe"

    "github.com/getlantern/systray"
    "github.com/yuin/gopher-lua"
    "golang.org/x/sys/windows"
)

const (
    enableAppendLog = false
    enableInfoLog   = true
    addonFolder = "./addons/ChatLogTranslator/"
    logFolder = addonFolder + "log/"
    reLogName = `^chat\d+\.txt$`
    translatedChatlogName = "translatedChat"
    infoLogFile = logFolder + "translation_info.txt"
    errorLogFile = logFolder + "translation_error.txt"
    deeplUrl = "https://api-free.deepl.com/v2/translate"
    gasUrl = "https://script.google.com/macros/s/"
    iconFile = addonFolder + "redria.ico"
    luaOptions = addonFolder + "options.lua"
    gameWindowTitle = "Ephinea: Phantasy Star Online Blue Burst"
    deeplMode = 1
    googleMode = 2
)

var deeplLanguages = []string{
    "EN-US",
    "EN-GB",
    "JA",
    "KO",
    "ZH-HANS",
    "ZH-HANT",
    "FR",
    "DE",
    "ES",
    "PT-BR",
    "RU",
    "IT",
    "TH",
    "VI",
    "ID",
    "AR",
}

var googleLanguages = []string{
    "en",
    "en",
    "ja",
    "ko",
    "zh-CN",
    "zh-TW",
    "fr",
    "de",
    "es",
    "pt-BR",
    "ru",
    "it",
    "th",
    "vi",
    "id",
    "ar",
}

//
// ----------------------- Logging -----------------------
//

func write(path string, message string) {
    if enableAppendLog {
        // appending line log
        f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
        if err != nil {
            fmt.Println("Failed to open logger file:", err)
            return
        }
        defer f.Close()
        _, _ = f.WriteString(message + "\n")
    } else {
        // one line log
        os.WriteFile(path, []byte(message + "\n"), 0644)
    }
}

func infoLog(args ...interface{}) {
    if !enableInfoLog {
        return
    }
    // appending line log
    timestamp := time.Now().Format("2006-01-02 15:04:05")
    f, err := os.OpenFile(infoLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        fmt.Println("Failed to open logger file:", err)
        return
    }
    defer f.Close()
    _, _ = f.WriteString(fmt.Sprint(timestamp, "\t" + fmt.Sprint(args...)) + "\n")
}

func errorLog(args ...interface{}) {
    timestamp := time.Now().Format("2006-01-02 15:04:05")
    write(errorLogFile, fmt.Sprint(timestamp, "\t" + fmt.Sprint(args...)))
}

func loadIcon() []byte {
    data, err := ioutil.ReadFile(iconFile)
    if err != nil {
        errorLog("Warning: failed to load icon. ")
        return nil
    } else {
        return data
    }
}

func findWindow(title string) (uintptr, error) {
    user32 := windows.NewLazySystemDLL("user32.dll")
    procFindWindow := user32.NewProc("FindWindowW")
    ptr := windows.StringToUTF16Ptr(title)

    hwnd, _, err := procFindWindow.Call(
        0, // class name (null)
        uintptr(unsafe.Pointer(ptr)),
    )
	if hwnd == 0 {
		return 0, err
	}
    return hwnd, nil
}

func messageBox(title, message string) {
    windows.MessageBox(
        0,
        windows.StringToUTF16Ptr(message),
        windows.StringToUTF16Ptr(title),
        windows.MB_OK|windows.MB_ICONWARNING)
}

//
// ----------------------- Main Entry -----------------------
//

func main() {

    // If PSOBB is not running, terminate the process.
	if _, err := findWindow(gameWindowTitle); err != nil {
        messageBox("Warning", "PSOBB is not running. Start the PSOBB first.")
        return
	}

    // Stop Dual Boot
    name, _ := windows.UTF16PtrFromString("Global\\TranlatorChatLogMutex")
    mutex, err := windows.CreateMutex(nil, false, name)
    if err != nil {
        // errorLog("Failed to create mutex. ", err)
        return
    }

    if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
        // errorLog("Another instance is already running. Exiting.")
        return
    }

    defer windows.ReleaseMutex(mutex)
    defer windows.CloseHandle(mutex)

    systray.Run(onReady, onExit)
}

func onReady() {
    infoLog("Application started...")

    systray.SetIcon(loadIcon())
    systray.SetTitle("Ephinea ChatLogTranslator")
    systray.SetTooltip("Ephinea ChatLogTranslator")

    mQuit := systray.AddMenuItem("Quit", "Quit the application")

    go func() {
        <-mQuit.ClickedCh
        systray.Quit()
    }()

    go func() {
        for {
            // If PSOBB is stopped, terminate the process.
            if _, err := findWindow(gameWindowTitle); err != nil {
                os.Exit(0)
            }

            checkAndTranslateFiles()
            // sleep for next roop
            time.Sleep(5 * time.Second)
        }
    }()
    
}

func onExit() {}

//
// ---------------------- File utilities ----------------------
//

func isSafeFileName(name string) bool {
    if name == "" {
        errorLog("File name is empty. Cannot delete. " + name)
        return false
    }
    if name == "." || name == ".." {
        errorLog("File name is only dot. Cannot delete. " + name)
        return false
    }
    if name == "/" || name == "\\" {
        errorLog("File name is only a slash or backslash. Cannot delete. " + name)
        return false
    }
    if strings.ContainsAny(name, `<>:"|?*`) {
        errorLog("File name contains invalid characters. Cannot delete. " + name)
        return false
    }
    // try to matching chat log regex
    base := filepath.Base(name)
    fileReg := regexp.MustCompile(reLogName)
    if !fileReg.MatchString(base) {
        errorLog("File name does not match the chat log pattern. Cannot delete. " + name)
        return false
    }
    return true
}

func getChatlogFilenames() []string {
    files, err := os.ReadDir(logFolder)
    if err != nil {
        errorLog("could not find log folder.")
        return nil
    }

    var filenames []string
    re := regexp.MustCompile(reLogName)
    for _, f := range files {
        if !f.IsDir() && re.MatchString(f.Name()) {
            filenames = append(filenames, f.Name())
        }
    }

    sort.Strings(filenames)
    return filenames
}

func readChatlogMessages(filenames []string) [][]string {
    var messages [][]string
    for _, filename := range filenames {
        f, err := os.Open(logFolder + filename)
        if err != nil {
            errorLog("Failed to open chatlog file.")
            continue
        }
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            parts := strings.Split(scanner.Text(), "\t")
            if len(parts) >= 3 {
                messages = append(messages, parts)
            }
        }
        f.Close()
    }
    return messages
}

func writeTranslatedLog(originalChat [][]string, results []string) {
    // today date time
    dateStr := time.Now().Format("20060102")
	outFile := fmt.Sprintf("%s%s%s.txt", logFolder, translatedChatlogName, dateStr)

    f, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        errorLog("Failed to open translated log file.")
        return
    }
    defer f.Close()

    for i, t := range results {
        line := originalChat[i][0] + "\t" + originalChat[i][1] + "\t" + originalChat[i][2] + "\t" + t + "\n"
        if _, err := f.WriteString(line); err != nil {
            errorLog("Failed to write to translated log file.")
            return
        }
    }
}

//
// ---------------------- Translation ----------------------
//

func translateByDeeplApi(messages [][]string, apiKey, language string) bool {
    infoLog("start deepL translate.")
    type DeepLResponse struct {
        Translations []struct {
            Text string `json:"text"`
        } `json:"translations"`
    }

    data := url.Values{}
    data.Set("auth_key", apiKey)
    data.Set("target_lang", language)

    for _, m := range messages {
        data.Add("text", m[2])
    }
    infoLog("request data:", data)

    // http request
    resp, err := http.Post(deeplUrl, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
    if err != nil {
        errorLog("translation request error. (DeepL)")
        return false
    }
    defer resp.Body.Close() 

    // check status
    if resp.StatusCode != http.StatusOK {
        errorLog("translation request error. please check your DeepL API Key.")
        return false
    }

    // check read body
    body, _ := io.ReadAll(resp.Body)
    var parsed DeepLResponse
    // check parse
    if err := json.Unmarshal(body, &parsed); err != nil {
        errorLog("failed to parse JSON response. (DeepL)")
        return false
    }

    // convert to array
    results := make([]string, len(parsed.Translations))
     for i, t := range parsed.Translations {
        results[i] = t.Text
    }

    infoLog("translation results:", results)
    // write log
    writeTranslatedLog(messages, results)
    return true
}

func translateByGas(messages [][]string, depId, language string) bool {
    infoLog("start google translate.")
    apiURL := gasUrl + depId + "/exec"

    infoLog("apiUrl:" + apiURL)
	texts := []string{}
    for _, m := range messages {
        texts = append(texts, m[2])
    }

    jsonData := map[string]interface{}{
        "texts":  texts,
        "target": language,
    }
    infoLog("request data:", jsonData)

    b, _ := json.Marshal(jsonData)
    resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(b))
	if err != nil {
        errorLog("translation request error. (Gas)")
		return false
	}
	defer resp.Body.Close()

	// check status
	if resp.StatusCode != http.StatusOK {
        errorLog("translation request error. please check your Google App Script Deplopment ID.")
		return false
	}

	body, _ := io.ReadAll(resp.Body)
    infoLog("response body:", body)
    // check parse
    var results []string
    if err := json.Unmarshal(body, &results); err != nil {
        errorLog("failed to parse JSON response. (Gas)")
        return false
    }

    infoLog("translation results:", results)
    // write log
    writeTranslatedLog(messages, results)
    return true
}

//
// ---------------------- Core ----------------------
//

func checkAndTranslateFiles() {
    infoLog("Translator loop triggered.")

    filenames := getChatlogFilenames()
    if len(filenames) == 0 {
        return
    }

    messages := readChatlogMessages(filenames)
    if len(messages) == 0 {
        return
    }

    infoLog("chatlog messages:", messages)

    // get apiKey
    apiKey, depId, language, transMode, err := loadLuaConfig()
    if err != nil {
        errorLog(err)
        return
    }

    var success bool
    switch transMode {
    case deeplMode:
        success = translateByDeeplApi(messages, apiKey, language)
    case googleMode:
        success = translateByGas(messages, depId, language)
    }

    if success {
        cleanupFiles(filenames)
    }
}

func loadLuaConfig() (apiKey, depId, language string, transMode int, err error) {
    infoLog("check lua options.")

    L := lua.NewState()
    defer L.Close()

    if err := L.DoFile(luaOptions); err != nil {
        return "", "", "", 0, fmt.Errorf("Please set up the the translator setting.")
    }

    tbl := L.Get(-1)
    t, ok := tbl.(*lua.LTable)
    if !ok {
        return "", "", "", 0, fmt.Errorf("Invalid Lua table format.")
    }

    langIndex := int(t.RawGetString("language").(lua.LNumber)) - 1
    transMode = int(t.RawGetString("translationMode").(lua.LNumber))

    switch transMode {
    case deeplMode:
        infoLog("check deepL transrator options.")
        apiKey = string(t.RawGetString("deeplApiKey").(lua.LString))
        if apiKey == "" {
            return "", "", "", transMode, fmt.Errorf("DeepL API Key not set.")
        }
        if langIndex >= 0 && langIndex < len(deeplLanguages) {
            language = deeplLanguages[langIndex]
        } else {
            language = "EN-US"
        }

    case googleMode:
        infoLog("check google transrator options.")
        depId = string(t.RawGetString("googleAppScriptDeploymentId").(lua.LString))
        if depId == "" {
            return "", "", "", transMode, fmt.Errorf("Google App Script Deployment ID not set.")
        }
        if langIndex >= 0 && langIndex < len(googleLanguages) {
            language = googleLanguages[langIndex]
        } else {
            language = "en"
        }

    default:
        return "", "", "", transMode, fmt.Errorf("translation mode not set.")
    }

    infoLog("lua options:", apiKey, ",", depId, ",", language, ",", transMode)
    return apiKey, depId, language, transMode, nil
}

func cleanupFiles(filenames []string) {
    infoLog("Translation successful. Cleaning up old files...")

    for _, filename := range filenames {
        path := logFolder + filename
        if !isSafeFileName(path) {
            errorLog("Unsafe file skipped:", path)
            continue
        }
        if err := os.Remove(path); err != nil {
            errorLog("Failed to delete:", path, err)
        }
    }

    if _, err := os.Stat(errorLogFile); err == nil {
        if err := os.Remove(errorLogFile); err != nil {
            errorLog("Failed to delete error log:", err)
        }
    }
}

