using System.ComponentModel;
using System.Windows;
using System.Windows.Controls;
using MirageClient.WPF.Models;
using MirageClient.WPF.Services;
using MirageClient.WPF.ViewModels;

namespace MirageClient.WPF;

public partial class MainWindow : Window
{
    private readonly MainViewModel _vm;
    private readonly CoreHostService _coreHostService;

    public MainViewModel ViewModel => _vm;

    public MainWindow(
        CoreApiClient coreApiClient,
        LocalizationService localizationService,
        SettingsService settingsService,
        CoreHostService coreHostService)
    {
        InitializeComponent();
        _coreHostService = coreHostService;
        _vm = new MainViewModel(coreApiClient, localizationService, settingsService);
        DataContext = _vm;
        Loaded += MainWindow_Loaded;
        Closing += MainWindow_Closing;
        StateChanged += MainWindow_StateChanged;
    }

    private async void MainWindow_Loaded(object sender, RoutedEventArgs e)
    {
        UpdateViewVisibility();
        await _coreHostService.EnsureRunningAsync();
        await _vm.RefreshAsync();
    }

    private void Overview_Click(object sender, RoutedEventArgs e)
    {
        _vm.Navigate("Overview");
        UpdateViewVisibility();
    }

    private void Profiles_Click(object sender, RoutedEventArgs e)
    {
        _vm.Navigate("Profiles");
        UpdateViewVisibility();
    }

    private async void Diagnostics_Click(object sender, RoutedEventArgs e)
    {
        _vm.Navigate("Diagnostics");
        UpdateViewVisibility();
        await _vm.RefreshDiagnosticsAsync();
    }

    private void Settings_Click(object sender, RoutedEventArgs e)
    {
        _vm.Navigate("Settings");
        UpdateViewVisibility();
    }

    private async void Import_Click(object sender, RoutedEventArgs e)
    {
        await _vm.ImportAsync();
        UpdateViewVisibility();
    }

    private async void Refresh_Click(object sender, RoutedEventArgs e)
    {
        await _vm.RefreshAsync();
    }

    private async void RefreshDiagnostics_Click(object sender, RoutedEventArgs e)
    {
        await _vm.RefreshAsync();
        await _vm.RefreshDiagnosticsAsync();
    }

    private void CopyDiagnostics_Click(object sender, RoutedEventArgs e)
    {
        if (!string.IsNullOrWhiteSpace(_vm.DiagnosticsReport))
        {
            System.Windows.Clipboard.SetText(_vm.DiagnosticsReport);
        }
    }

    private async void ProfileToggle_Click(object sender, RoutedEventArgs e)
    {
        if ((sender as FrameworkElement)?.Tag is ProfileItem item)
        {
            await _vm.ToggleConnectionAsync(item);
        }
    }

    private void Language_Changed(object sender, SelectionChangedEventArgs e)
    {
        // The binding already updates persisted settings.
    }

    private async void ApplyProxy_Click(object sender, RoutedEventArgs e)
    {
        await _vm.ApplyProxySettingsAsync();
    }

    private async void ReapplyProxy_Click(object sender, RoutedEventArgs e)
    {
        await _vm.ReapplyProxyAsync();
    }

    private void OpenPac_Click(object sender, RoutedEventArgs e)
    {
        _vm.OpenPacInBrowser();
    }

    private void MainWindow_Closing(object? sender, CancelEventArgs e)
    {
        if (System.Windows.Application.Current is App app && !app.HandleWindowClosing(this))
        {
            e.Cancel = true;
        }
    }

    private void MainWindow_StateChanged(object? sender, EventArgs e)
    {
        if (System.Windows.Application.Current is App app)
        {
            app.HandleWindowStateChanged(this);
        }
    }

    private void UpdateViewVisibility()
    {
        OverviewPanel.Visibility = _vm.CurrentView == "Overview" ? Visibility.Visible : Visibility.Collapsed;
        ProfilesPanel.Visibility = _vm.CurrentView == "Profiles" ? Visibility.Visible : Visibility.Collapsed;
        DiagnosticsPanel.Visibility = _vm.CurrentView == "Diagnostics" ? Visibility.Visible : Visibility.Collapsed;
        SettingsPanel.Visibility = _vm.CurrentView == "Settings" ? Visibility.Visible : Visibility.Collapsed;
    }
}
