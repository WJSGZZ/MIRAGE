using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Diagnostics;
using System.Linq;
using System.Runtime.CompilerServices;
using System.Windows.Threading;
using MirageClient.WPF.Models;
using MirageClient.WPF.Services;

namespace MirageClient.WPF.ViewModels;

public sealed class MainViewModel : INotifyPropertyChanged
{
    private readonly CoreApiClient _core;
    private readonly LocalizationService _loc;
    private readonly SettingsService _settingsService;
    private readonly ClientSettings _settings;
    private readonly DispatcherTimer _timer;

    private string _currentView = "Overview";
    private string _statusText = "";
    private bool _isConnected;
    private string _httpProxy = "-";
    private string _socks5Proxy = "-";
    private string _upload = "0 B";
    private string _download = "0 B";
    private string _rate = "0 B/s";
    private string _coreVersion = "-";
    private string _protocolVersion = "-";
    private string _healthText = "-";
    private string _importUri = "";
    private string _proxyMode = "system";
    private bool _proxyApplied;
    private bool _applyWinHttp = true;
    private bool _exportEnv = true;
    private string _pacUrl = "-";
    private string _proxyApplyStatus = "-";
    private string _activeProfileName = "-";
    private string _winHttpStatus = "-";
    private string _envStatus = "-";
    private string _pacStatus = "-";
    private string _diagnosticsReport = "";
    private string? _lastLogTimestamp;

    public MainViewModel(CoreApiClient core, LocalizationService loc, SettingsService settingsService)
    {
        _core = core;
        _loc = loc;
        _settingsService = settingsService;
        _settings = settingsService.Load();

        Profiles = new ObservableCollection<ProfileItem>();
        Logs = new ObservableCollection<string>();
        LanguageModes = new ObservableCollection<LanguageChoice>();
        ProxyModes = new ObservableCollection<ProxyModeChoice>();

        _loc.SetLanguage(_settings.ResolveUiLanguage());
        _loc.LanguageChanged += (_, _) => RefreshLocalizedText();

        ReloadLanguageModes();
        ReloadProxyModes();
        RefreshLocalizedText();

        _timer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(2) };
        _timer.Tick += async (_, _) => await RefreshAsync();
        _timer.Start();
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    public ObservableCollection<ProfileItem> Profiles { get; }
    public ObservableCollection<string> Logs { get; }
    public ObservableCollection<LanguageChoice> LanguageModes { get; }
    public ObservableCollection<ProxyModeChoice> ProxyModes { get; }

    public string CurrentView
    {
        get => _currentView;
        set => SetField(ref _currentView, value);
    }

    public string SelectedLanguageMode
    {
        get => _settings.LanguageMode;
        set
        {
            if (_settings.LanguageMode == value)
            {
                return;
            }

            _settings.LanguageMode = value;
            SaveSettings();
            _loc.SetLanguage(_settings.ResolveUiLanguage());
            OnPropertyChanged();
        }
    }

    public bool CloseToTray
    {
        get => _settings.CloseToTray;
        set
        {
            if (_settings.CloseToTray == value)
            {
                return;
            }

            _settings.CloseToTray = value;
            SaveSettings();
            OnPropertyChanged();
        }
    }

    public bool MinimizeToTray
    {
        get => _settings.MinimizeToTray;
        set
        {
            if (_settings.MinimizeToTray == value)
            {
                return;
            }

            _settings.MinimizeToTray = value;
            SaveSettings();
            OnPropertyChanged();
        }
    }

    public bool StartMinimizedToTray
    {
        get => _settings.StartMinimizedToTray;
        set
        {
            if (_settings.StartMinimizedToTray == value)
            {
                return;
            }

            _settings.StartMinimizedToTray = value;
            SaveSettings();
            OnPropertyChanged();
        }
    }

    public string StatusText { get => _statusText; private set => SetField(ref _statusText, value); }
    public bool IsConnected { get => _isConnected; private set => SetField(ref _isConnected, value); }
    public string HttpProxy { get => _httpProxy; private set => SetField(ref _httpProxy, value); }
    public string Socks5Proxy { get => _socks5Proxy; private set => SetField(ref _socks5Proxy, value); }
    public string Upload { get => _upload; private set => SetField(ref _upload, value); }
    public string Download { get => _download; private set => SetField(ref _download, value); }
    public string Rate { get => _rate; private set => SetField(ref _rate, value); }
    public string CoreVersion { get => _coreVersion; private set => SetField(ref _coreVersion, value); }
    public string ProtocolVersion { get => _protocolVersion; private set => SetField(ref _protocolVersion, value); }
    public string HealthText { get => _healthText; private set => SetField(ref _healthText, value); }
    public string ImportUri { get => _importUri; set => SetField(ref _importUri, value); }
    public string SelectedProxyMode
    {
        get => _proxyMode;
        set
        {
            if (SetField(ref _proxyMode, value))
            {
                OnPropertyChanged(nameof(CurrentProxyModeText));
            }
        }
    }
    public bool ProxyApplied
    {
        get => _proxyApplied;
        private set
        {
            if (SetField(ref _proxyApplied, value))
            {
                OnPropertyChanged(nameof(ProxyAppliedText));
            }
        }
    }
    public bool ApplyWinHttp { get => _applyWinHttp; set => SetField(ref _applyWinHttp, value); }
    public bool ExportEnv { get => _exportEnv; set => SetField(ref _exportEnv, value); }
    public string PacUrl { get => _pacUrl; private set => SetField(ref _pacUrl, value); }
    public string ProxyApplyStatus { get => _proxyApplyStatus; private set => SetField(ref _proxyApplyStatus, value); }
    public string ActiveProfileName { get => _activeProfileName; private set => SetField(ref _activeProfileName, value); }
    public string WinHttpStatus { get => _winHttpStatus; private set => SetField(ref _winHttpStatus, value); }
    public string EnvStatus { get => _envStatus; private set => SetField(ref _envStatus, value); }
    public string PacStatus { get => _pacStatus; private set => SetField(ref _pacStatus, value); }
    public string DiagnosticsReport { get => _diagnosticsReport; private set => SetField(ref _diagnosticsReport, value); }

    public string CurrentProxyModeText => ProxyModeTextFor(SelectedProxyMode);
    public string ProxyAppliedText => ProxyApplied ? Label("Applied") : Label("NotApplied");

    public string AppTitle => Label("AppTitle");
    public string Tagline => Label("Tagline");
    public string OverviewLabel => Label("Overview");
    public string ProfilesLabel => Label("Profiles");
    public string DiagnosticsLabel => Label("Diagnostics");
    public string SettingsLabel => Label("Settings");
    public string OverviewTitle => Label("OverviewTitle");
    public string OverviewDescription => Label("OverviewDesc");
    public string ProfilesTitle => Label("ProfileListTitle");
    public string ProfilesDescription => Label("ProfileListDesc");
    public string DiagnosticsTitle => Label("DiagnosticsTitle");
    public string DiagnosticsDescription => Label("DiagnosticsDesc");
    public string SettingsTitle => Label("SettingsTitle");
    public string SettingsDescription => Label("SettingsDesc");
    public string HttpLabel => Label("HttpProxy");
    public string SocksLabel => Label("Socks5");
    public string UploadLabel => Label("Upload");
    public string DownloadLabel => Label("Download");
    public string RateLabel => Label("Rate");
    public string ImportLabel => Label("Import");
    public string RefreshLabel => Label("Refresh");
    public string ConnectLabel => Label("Connect");
    public string DisconnectLabel => Label("Disconnect");
    public string LanguageLabel => Label("Language");
    public string SystemProxyLabel => Label("SystemProxy");
    public string ImportHint => Label("ImportHint");
    public string CaptureGap => Label("CaptureGap");
    public string NoProfilesText => Label("NoProfiles");
    public string LogEmptyText => Label("LogEmpty");
    public string CoreVersionLabel => Label("CoreVersion");
    public string ProtocolVersionLabel => Label("ProtocolVersion");
    public string HealthLabel => Label("Health");
    public string ProxyModeLabel => Label("ProxyMode");
    public string ApplyWinHttpLabel => Label("ApplyWinHttp");
    public string ExportEnvLabel => Label("ExportEnv");
    public string PacAddressLabel => Label("PacAddress");
    public string ProxyAppliedLabel => Label("ProxyApplied");
    public string LastProxyApplyLabel => Label("LastProxyApply");
    public string ReapplyProxyLabel => Label("ReapplyProxy");
    public string ApplyProxyLabel => Label("ApplyProxy");
    public string OpenPacLabel => Label("OpenPac");
    public string CurrentProfileLabel => Label("CurrentProfile");
    public string WinHttpStateLabel => Label("WinHttpState");
    public string EnvStateLabel => Label("EnvState");
    public string PacStateLabel => Label("PacState");
    public string ProxySummaryLabel => Label("ProxySummary");
    public string TrafficSummaryLabel => Label("TrafficSummary");
    public string QuickImportLabel => Label("QuickImport");
    public string TrayBehaviorLabel => Label("TrayBehavior");
    public string CloseToTrayLabel => Label("CloseToTray");
    public string MinimizeToTrayLabel => Label("MinimizeToTray");
    public string StartMinimizedToTrayLabel => Label("StartMinimizedToTray");
    public string DiagnosticsReportLabel => Label("DiagnosticsReport");
    public string CopyDiagnosticsLabel => Label("CopyDiagnostics");
    public string DiagnosticsTargetHint => Label("DiagnosticsTargetHint");
    public string SystemPolicyLabel => Label("SystemPolicy");
    public string OpenProfilesLabel => Label("OpenProfiles");

    public void Navigate(string view) => CurrentView = view;

    public async Task RefreshAsync()
    {
        try
        {
            using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(3));
            var healthTask = _core.GetHealthAsync(cts.Token);
            var versionTask = _core.GetVersionAsync(cts.Token);
            var stateTask = _core.GetStateAsync(cts.Token);
            var statsTask = _core.GetStatsAsync(cts.Token);
            var profilesTask = _core.GetProfilesAsync(cts.Token);
            var logsTask = _core.GetLogsAsync(_lastLogTimestamp, cts.Token);
            var proxyTask = _core.GetProxyConfigAsync(cts.Token);

            await Task.WhenAll(healthTask, versionTask, stateTask, statsTask, profilesTask, logsTask, proxyTask);

            var health = healthTask.Result;
            var version = versionTask.Result;
            var state = stateTask.Result;
            var stats = statsTask.Result;
            var profiles = profilesTask.Result;
            var logs = logsTask.Result;
            var proxy = proxyTask.Result;

            IsConnected = state?.Running == true;
            StatusText = IsConnected ? Label("StatusOnline") : Label("StatusOffline");
            HttpProxy = string.IsNullOrWhiteSpace(state?.Http) ? "-" : state!.Http;
            Socks5Proxy = string.IsNullOrWhiteSpace(state?.Socks5) ? "-" : state!.Socks5;
            Upload = FormatBytes(stats?.UploadBytes ?? 0);
            Download = FormatBytes(stats?.DownloadBytes ?? 0);
            Rate = $"{FormatBytes(stats?.UploadRateBps ?? 0)}/s up  {FormatBytes(stats?.DownloadRateBps ?? 0)}/s down";
            CoreVersion = version?.Core ?? "-";
            ProtocolVersion = version?.Protocol ?? "-";
            HealthText = health?.Ready == true ? Label("Ready") : Label("Running");

            Profiles.Clear();
            foreach (var item in profiles)
            {
                Profiles.Add(item);
            }

            ActiveProfileName = Profiles.FirstOrDefault(x => x.Active)?.Name ?? Label("NoActiveProfile");

            if (proxy is not null)
            {
                SelectedProxyMode = proxy.Mode;
                ApplyWinHttp = proxy.ApplyWinHttp;
                ExportEnv = proxy.ExportEnv;
                PacUrl = string.IsNullOrWhiteSpace(proxy.PacUrl) ? "-" : proxy.PacUrl;
                ProxyApplied = proxy.Applied;
                ProxyApplyStatus = string.IsNullOrWhiteSpace(proxy.LastApplyError)
                    ? string.IsNullOrWhiteSpace(proxy.LastApplyAt) ? "-" : proxy.LastApplyAt
                    : proxy.LastApplyError;
                WinHttpStatus = IsWinHttpActive(proxy.System) ? Label("Enabled") : Label("Disabled");
                EnvStatus = HasProxyEnv(proxy.System) ? Label("Enabled") : Label("Disabled");
                PacStatus = proxy.PacRunning ? Label("PacRunning") : Label("PacInactive");
            }
            else
            {
                WinHttpStatus = "-";
                EnvStatus = "-";
                PacStatus = "-";
            }

            if (logs?.Items is { Count: > 0 })
            {
                foreach (var item in logs.Items)
                {
                    _lastLogTimestamp = item.Timestamp;
                    Logs.Add($"[{FormatTimestamp(item.Timestamp)}] {item.Message}");
                }

                while (Logs.Count > 200)
                {
                    Logs.RemoveAt(0);
                }
            }
        }
        catch (Exception ex)
        {
            StatusText = ex.Message;
        }
    }

    public async Task RefreshDiagnosticsAsync()
    {
        try
        {
            using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
            DiagnosticsReport = await _core.GetDiagnosticsTextAsync("https://api.openai.com", cts.Token);
        }
        catch (Exception ex)
        {
            DiagnosticsReport = ex.Message;
        }
    }

    public async Task ImportAsync()
    {
        if (string.IsNullOrWhiteSpace(ImportUri))
        {
            return;
        }

        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        await _core.ImportAsync(ImportUri.Trim(), cts.Token);
        ImportUri = "";
        await RefreshAsync();
        CurrentView = "Profiles";
    }

    public async Task ToggleConnectionAsync(ProfileItem item)
    {
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(8));
        if (item.Active)
        {
            await _core.DisconnectAsync(cts.Token);
        }
        else
        {
            await _core.ConnectAsync(item.Id, cts.Token);
        }

        await RefreshAsync();
        if (CurrentView == "Diagnostics")
        {
            await RefreshDiagnosticsAsync();
        }
    }

    public async Task ApplyProxySettingsAsync()
    {
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        await _core.UpdateProxyConfigAsync(new ProxyConfigUpdate
        {
            Mode = SelectedProxyMode,
            ApplyWinHttp = ApplyWinHttp,
            ExportEnv = ExportEnv,
        }, cts.Token);
        await RefreshAsync();
        if (CurrentView == "Diagnostics")
        {
            await RefreshDiagnosticsAsync();
        }
    }

    public async Task ReapplyProxyAsync()
    {
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        await _core.ReapplyProxyAsync(cts.Token);
        await RefreshAsync();
        if (CurrentView == "Diagnostics")
        {
            await RefreshDiagnosticsAsync();
        }
    }

    public async Task ConnectFromTrayAsync()
    {
        await RefreshAsync();
        var active = Profiles.FirstOrDefault(x => x.Active);
        if (active is not null)
        {
            return;
        }

        if (Profiles.Count == 1)
        {
            await ToggleConnectionAsync(Profiles[0]);
            return;
        }

        CurrentView = "Profiles";
    }

    public async Task DisconnectFromTrayAsync()
    {
        await RefreshAsync();
        var active = Profiles.FirstOrDefault(x => x.Active);
        if (active is not null)
        {
            await ToggleConnectionAsync(active);
            return;
        }

        if (IsConnected)
        {
            using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
            await _core.DisconnectAsync(cts.Token);
            await RefreshAsync();
        }
    }

    public void OpenPacInBrowser()
    {
        if (string.IsNullOrWhiteSpace(PacUrl) || PacUrl == "-")
        {
            return;
        }

        Process.Start(new ProcessStartInfo
        {
            FileName = PacUrl,
            UseShellExecute = true,
        });
    }

    private void RefreshLocalizedText()
    {
        ReloadLanguageModes();
        ReloadProxyModes();

        OnPropertyChanged(nameof(AppTitle));
        OnPropertyChanged(nameof(Tagline));
        OnPropertyChanged(nameof(OverviewLabel));
        OnPropertyChanged(nameof(ProfilesLabel));
        OnPropertyChanged(nameof(DiagnosticsLabel));
        OnPropertyChanged(nameof(SettingsLabel));
        OnPropertyChanged(nameof(OverviewTitle));
        OnPropertyChanged(nameof(OverviewDescription));
        OnPropertyChanged(nameof(ProfilesTitle));
        OnPropertyChanged(nameof(ProfilesDescription));
        OnPropertyChanged(nameof(DiagnosticsTitle));
        OnPropertyChanged(nameof(DiagnosticsDescription));
        OnPropertyChanged(nameof(SettingsTitle));
        OnPropertyChanged(nameof(SettingsDescription));
        OnPropertyChanged(nameof(HttpLabel));
        OnPropertyChanged(nameof(SocksLabel));
        OnPropertyChanged(nameof(UploadLabel));
        OnPropertyChanged(nameof(DownloadLabel));
        OnPropertyChanged(nameof(RateLabel));
        OnPropertyChanged(nameof(ImportLabel));
        OnPropertyChanged(nameof(RefreshLabel));
        OnPropertyChanged(nameof(ConnectLabel));
        OnPropertyChanged(nameof(DisconnectLabel));
        OnPropertyChanged(nameof(LanguageLabel));
        OnPropertyChanged(nameof(SystemProxyLabel));
        OnPropertyChanged(nameof(ImportHint));
        OnPropertyChanged(nameof(CaptureGap));
        OnPropertyChanged(nameof(NoProfilesText));
        OnPropertyChanged(nameof(LogEmptyText));
        OnPropertyChanged(nameof(CoreVersionLabel));
        OnPropertyChanged(nameof(ProtocolVersionLabel));
        OnPropertyChanged(nameof(HealthLabel));
        OnPropertyChanged(nameof(ProxyModeLabel));
        OnPropertyChanged(nameof(ApplyWinHttpLabel));
        OnPropertyChanged(nameof(ExportEnvLabel));
        OnPropertyChanged(nameof(PacAddressLabel));
        OnPropertyChanged(nameof(ProxyAppliedLabel));
        OnPropertyChanged(nameof(ProxyAppliedText));
        OnPropertyChanged(nameof(LastProxyApplyLabel));
        OnPropertyChanged(nameof(ReapplyProxyLabel));
        OnPropertyChanged(nameof(ApplyProxyLabel));
        OnPropertyChanged(nameof(OpenPacLabel));
        OnPropertyChanged(nameof(CurrentProfileLabel));
        OnPropertyChanged(nameof(WinHttpStateLabel));
        OnPropertyChanged(nameof(EnvStateLabel));
        OnPropertyChanged(nameof(PacStateLabel));
        OnPropertyChanged(nameof(ProxySummaryLabel));
        OnPropertyChanged(nameof(TrafficSummaryLabel));
        OnPropertyChanged(nameof(QuickImportLabel));
        OnPropertyChanged(nameof(TrayBehaviorLabel));
        OnPropertyChanged(nameof(CloseToTrayLabel));
        OnPropertyChanged(nameof(MinimizeToTrayLabel));
        OnPropertyChanged(nameof(StartMinimizedToTrayLabel));
        OnPropertyChanged(nameof(DiagnosticsReportLabel));
        OnPropertyChanged(nameof(CopyDiagnosticsLabel));
        OnPropertyChanged(nameof(DiagnosticsTargetHint));
        OnPropertyChanged(nameof(SystemPolicyLabel));
        OnPropertyChanged(nameof(OpenProfilesLabel));
        OnPropertyChanged(nameof(CurrentProxyModeText));

        StatusText = IsConnected ? Label("StatusOnline") : Label("StatusOffline");
    }

    private void ReloadLanguageModes()
    {
        var selected = _settings.LanguageMode;
        LanguageModes.Clear();
        LanguageModes.Add(new LanguageChoice("system", Label("LanguageSystem")));
        LanguageModes.Add(new LanguageChoice("zh-CN", Label("LanguageZh")));
        LanguageModes.Add(new LanguageChoice("en-US", Label("LanguageEn")));
        OnPropertyChanged(nameof(LanguageModes));
        OnPropertyChanged(nameof(SelectedLanguageMode));
        _settings.LanguageMode = selected;
    }

    private void ReloadProxyModes()
    {
        var selected = SelectedProxyMode;
        ProxyModes.Clear();
        ProxyModes.Add(new ProxyModeChoice("off", Label("ProxyModeOff")));
        ProxyModes.Add(new ProxyModeChoice("system", Label("ProxyModeSystem")));
        ProxyModes.Add(new ProxyModeChoice("manual", Label("ProxyModeManual")));
        ProxyModes.Add(new ProxyModeChoice("pac", Label("ProxyModePac")));
        OnPropertyChanged(nameof(ProxyModes));
        if (!string.IsNullOrWhiteSpace(selected))
        {
            SelectedProxyMode = selected;
        }
    }

    private string ProxyModeTextFor(string mode)
    {
        return mode switch
        {
            "off" => Label("ProxyModeOff"),
            "manual" => Label("ProxyModeManual"),
            "pac" => Label("ProxyModePac"),
            _ => Label("ProxyModeSystem"),
        };
    }

    private string Label(string key) => _loc.T(key);

    private void SaveSettings()
    {
        _settingsService.Save(_settings);
    }

    private static bool IsWinHttpActive(ProxySystemSnapshot snapshot)
    {
        var text = (snapshot.WinHttp ?? "").ToLowerInvariant();
        return !string.IsNullOrWhiteSpace(text) &&
               !text.Contains("direct access") &&
               !text.Contains("直接访问");
    }

    private static bool HasProxyEnv(ProxySystemSnapshot snapshot)
    {
        return snapshot.Env.TryGetValue("HTTP_PROXY", out var http) && !string.IsNullOrWhiteSpace(http) ||
               snapshot.Env.TryGetValue("HTTPS_PROXY", out var https) && !string.IsNullOrWhiteSpace(https) ||
               snapshot.Env.TryGetValue("ALL_PROXY", out var all) && !string.IsNullOrWhiteSpace(all);
    }

    private static string FormatBytes(long value)
    {
        string[] units = ["B", "KB", "MB", "GB", "TB"];
        double n = value;
        var index = 0;
        while (n >= 1024 && index < units.Length - 1)
        {
            n /= 1024;
            index++;
        }

        var digits = index == 0 ? 0 : n < 10 ? 1 : 0;
        return $"{n.ToString($"F{digits}")} {units[index]}";
    }

    private static string FormatTimestamp(string value)
    {
        return DateTime.TryParse(value, out var t)
            ? t.ToLocalTime().ToString("HH:mm:ss")
            : value;
    }

    private bool SetField<T>(ref T field, T value, [CallerMemberName] string? propertyName = null)
    {
        if (EqualityComparer<T>.Default.Equals(field, value))
        {
            return false;
        }

        field = value;
        OnPropertyChanged(propertyName);
        return true;
    }

    private void OnPropertyChanged([CallerMemberName] string? propertyName = null)
    {
        PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
    }
}

public sealed class LanguageChoice
{
    public LanguageChoice(string value, string label)
    {
        Value = value;
        Label = label;
    }

    public string Value { get; }
    public string Label { get; }
}

public sealed class ProxyModeChoice
{
    public ProxyModeChoice(string value, string label)
    {
        Value = value;
        Label = label;
    }

    public string Value { get; }
    public string Label { get; }
}
