using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Security.Cryptography;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using Hardcodet.Wpf.TaskbarNotification;
using MirageClient.WPF.Services;

namespace MirageClient.WPF;

public partial class App : System.Windows.Application
{
    private Mutex? _singleInstanceMutex;
    private EventWaitHandle? _activationEvent;
    private CancellationTokenSource? _activationLoopCts;
    private CancellationTokenSource? _startupCts;
    private TaskbarIcon? _trayIcon;
    private SettingsService? _settingsService;
    private LocalizationService? _loc;
    private ClientSettings? _settings;
    private CoreApiClient? _coreApiClient;
    private CoreHostService? _coreHostService;
    private bool _allowExit;
    private string? _startupLogPath;

    public bool AllowExit => _allowExit;

    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);

        _startupLogPath = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
            "MIRAGE",
            "MirageClient.WPF",
            "startup.log");
        Directory.CreateDirectory(Path.GetDirectoryName(_startupLogPath)!);
        AppDomain.CurrentDomain.UnhandledException += (_, args) => WriteStartupLog("AppDomain", args.ExceptionObject);
        DispatcherUnhandledException += (_, args) =>
        {
            WriteStartupLog("Dispatcher", args.Exception);
        };
        TaskScheduler.UnobservedTaskException += (_, args) =>
        {
            WriteStartupLog("Task", args.Exception);
            args.SetObserved();
        };

        _settingsService = new SettingsService();
        _settings = _settingsService.Load();
        _loc = new LocalizationService();
        _loc.SetLanguage(_settings.ResolveUiLanguage());
        _loc.LanguageChanged += (_, _) => LocalizeTrayMenu();
        _coreApiClient = new CoreApiClient();
        _coreHostService = new CoreHostService();

        if (!AcquireSingleInstance())
        {
            Shutdown();
            return;
        }

        SetupTrayIcon();

        _startupCts = new CancellationTokenSource();
        _ = _coreHostService.EnsureRunningAsync(_startupCts.Token);

        var window = new MainWindow(_coreApiClient, _loc, _settingsService, _coreHostService);
        MainWindow = window;

        if (_settings.StartMinimizedToTray)
        {
            window.Hide();
            window.ShowInTaskbar = false;
        }
        else
        {
            window.Show();
        }
    }

    protected override void OnExit(ExitEventArgs e)
    {
        _startupCts?.Cancel();
        _activationLoopCts?.Cancel();

        if (_trayIcon is not null)
        {
            _trayIcon.Visibility = Visibility.Collapsed;
            _trayIcon.Dispose();
            _trayIcon = null;
        }

        if (_coreHostService is not null)
        {
            try
            {
                _coreHostService.ShutdownAsync().GetAwaiter().GetResult();
            }
            catch
            {
                // Best effort shutdown only.
            }

            _coreHostService.Dispose();
            _coreHostService = null;
        }

        _activationEvent?.Dispose();
        _singleInstanceMutex?.Dispose();

        base.OnExit(e);
    }

    public bool HandleWindowClosing(Window window)
    {
        if (_allowExit)
        {
            return true;
        }

        if (_settings?.CloseToTray != false)
        {
            HideMainWindow(window);
            return false;
        }

        return true;
    }

    public void HandleWindowStateChanged(Window window)
    {
        if (_settings?.MinimizeToTray == true && window.WindowState == WindowState.Minimized)
        {
            HideMainWindow(window);
        }
    }

    public void ShowMainWindow()
    {
        if (MainWindow is null)
        {
            return;
        }

        MainWindow.ShowInTaskbar = true;
        if (!MainWindow.IsVisible)
        {
            MainWindow.Show();
        }

        if (MainWindow.WindowState == WindowState.Minimized)
        {
            MainWindow.WindowState = WindowState.Normal;
        }

        MainWindow.Activate();
        MainWindow.Topmost = true;
        MainWindow.Topmost = false;
        MainWindow.Focus();
    }

    public void RequestExit()
    {
        _allowExit = true;
        Shutdown();
    }

    private bool AcquireSingleInstance()
    {
        var id = ComputeInstanceId();
        _singleInstanceMutex = new Mutex(true, $@"Global\MIRAGE_{id}", out var createdNew);
        _activationEvent = new EventWaitHandle(false, EventResetMode.AutoReset, $@"Global\MIRAGE_ACTIVATE_{id}");

        if (!createdNew)
        {
            try
            {
                _activationEvent.Set();
            }
            catch
            {
                // Best effort only.
            }

            return false;
        }

        _activationLoopCts = new CancellationTokenSource();
        _ = Task.Run(() => WaitForActivationAsync(_activationLoopCts.Token));
        return true;
    }

    private async Task WaitForActivationAsync(CancellationToken ct)
    {
        if (_activationEvent is null)
        {
            return;
        }

        while (!ct.IsCancellationRequested)
        {
            try
            {
                var signaled = _activationEvent.WaitOne(TimeSpan.FromMilliseconds(500));
                if (!signaled)
                {
                    continue;
                }

                await Dispatcher.InvokeAsync(ShowMainWindow);
            }
            catch
            {
                return;
            }
        }
    }

    private void SetupTrayIcon()
    {
        _trayIcon = FindResource("TrayIcon") as TaskbarIcon;
        if (_trayIcon is null || _loc is null)
        {
            return;
        }

        _trayIcon.Icon = SystemIcons.Application;
        _trayIcon.ToolTipText = _loc.T("AppTitle");
        LocalizeTrayMenu();
    }

    private void LocalizeTrayMenu()
    {
        if (_trayIcon?.ContextMenu is not ContextMenu menu || _loc is null)
        {
            return;
        }

        if (menu.FindName("TrayOpenMenuItem") is MenuItem openItem)
        {
            openItem.Header = _loc.T("TrayOpen");
        }

        if (menu.FindName("TrayConnectMenuItem") is MenuItem connectItem)
        {
            connectItem.Header = _loc.T("TrayConnect");
        }

        if (menu.FindName("TrayDisconnectMenuItem") is MenuItem disconnectItem)
        {
            disconnectItem.Header = _loc.T("TrayDisconnect");
        }

        if (menu.FindName("TrayProxyModeMenuItem") is MenuItem proxyModeItem)
        {
            proxyModeItem.Header = _loc.T("TrayProxyMode");
        }

        if (menu.FindName("TrayProxyModeOffMenuItem") is MenuItem offItem)
        {
            offItem.Header = _loc.T("TrayModeOff");
        }

        if (menu.FindName("TrayProxyModeSystemMenuItem") is MenuItem systemItem)
        {
            systemItem.Header = _loc.T("TrayModeSystem");
        }

        if (menu.FindName("TrayProxyModeManualMenuItem") is MenuItem manualItem)
        {
            manualItem.Header = _loc.T("TrayModeManual");
        }

        if (menu.FindName("TrayProxyModePacMenuItem") is MenuItem pacItem)
        {
            pacItem.Header = _loc.T("TrayModePac");
        }

        if (menu.FindName("TrayReapplyProxyMenuItem") is MenuItem reapplyItem)
        {
            reapplyItem.Header = _loc.T("TrayProxyReapply");
        }

        if (menu.FindName("TrayExitMenuItem") is MenuItem exitItem)
        {
            exitItem.Header = _loc.T("TrayExit");
        }
    }

    private MainWindow? MainClientWindow => MainWindow as MainWindow;

    private async void TrayOpen_Click(object sender, RoutedEventArgs e)
    {
        ShowMainWindow();
        if (MainClientWindow is not null)
        {
            await MainClientWindow.ViewModel.RefreshAsync();
        }
    }

    private async void TrayConnect_Click(object sender, RoutedEventArgs e)
    {
        if (MainClientWindow is null)
        {
            return;
        }

        await MainClientWindow.ViewModel.ConnectFromTrayAsync();
        ShowMainWindow();
    }

    private async void TrayDisconnect_Click(object sender, RoutedEventArgs e)
    {
        if (MainClientWindow is null)
        {
            return;
        }

        await MainClientWindow.ViewModel.DisconnectFromTrayAsync();
    }

    private async void TrayProxyMode_Click(object sender, RoutedEventArgs e)
    {
        if (MainClientWindow is null || sender is not MenuItem menuItem || menuItem.Tag is not string mode)
        {
            return;
        }

        MainClientWindow.ViewModel.SelectedProxyMode = mode;
        await MainClientWindow.ViewModel.ApplyProxySettingsAsync();
    }

    private async void TrayReapplyProxy_Click(object sender, RoutedEventArgs e)
    {
        if (MainClientWindow is null)
        {
            return;
        }

        await MainClientWindow.ViewModel.ReapplyProxyAsync();
    }

    private void TrayExit_Click(object sender, RoutedEventArgs e)
    {
        RequestExit();
    }

    private static string ComputeInstanceId()
    {
        var exe = Process.GetCurrentProcess().MainModule?.FileName ?? "MirageClient";
        var bytes = SHA256.HashData(Encoding.UTF8.GetBytes(exe.ToLowerInvariant()));
        return Convert.ToHexString(bytes[..8]);
    }

    private static void HideMainWindow(Window window)
    {
        window.ShowInTaskbar = false;
        window.Hide();
    }

    private void WriteStartupLog(string source, object? error)
    {
        try
        {
            if (string.IsNullOrWhiteSpace(_startupLogPath))
            {
                return;
            }

            File.AppendAllText(
                _startupLogPath,
                $"[{DateTime.Now:yyyy-MM-dd HH:mm:ss}] {source}{Environment.NewLine}{error}{Environment.NewLine}{Environment.NewLine}");
        }
        catch
        {
            // Ignore logging failures.
        }
    }
}
