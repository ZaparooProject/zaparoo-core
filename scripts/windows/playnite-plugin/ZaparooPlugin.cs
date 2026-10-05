// Zaparoo Playnite Extension
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Linq;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using Playnite.SDK;
using Playnite.SDK.Data;
using Playnite.SDK.Events;
using Playnite.SDK.Models;
using Playnite.SDK.Plugins;

namespace ZaparooPlaynite
{
    // Wire messages. Property names are the JSON keys Zaparoo Core reads and
    // writes; see pkg/platforms/shared/playnite/protocol.go.

    public class PlatformInfo
    {
        public string SpecificationId { get; set; }
        public string Name { get; set; }
    }

    public class GameInfo
    {
        public string Id { get; set; }
        public string Name { get; set; }
        public string LibraryPluginId { get; set; }
        public string RomPath { get; set; }
        public string Description { get; set; }
        public string Cover { get; set; }
        public string Background { get; set; }
        public string Icon { get; set; }
        public List<PlatformInfo> Platforms { get; set; }
        public List<string> Developers { get; set; }
        public List<string> Publishers { get; set; }
        public List<string> Genres { get; set; }
        public int ReleaseYear { get; set; }
        public bool IsInstalled { get; set; }
        public bool Hidden { get; set; }
    }

    public class CoreCommand
    {
        public string Command { get; set; }
        public string Id { get; set; }
        public string RequestId { get; set; }
        public bool Details { get; set; }
    }

    public class PluginEvent
    {
        public string Event { get; set; }
        public string Id { get; set; }
        public string Name { get; set; }
        public string RequestId { get; set; }
        public string Status { get; set; }
        public string Error { get; set; }
        public string Command { get; set; }
        public string Exe { get; set; }
        public string PluginVersion { get; set; }
        public string PlayniteVersion { get; set; }
        public string Mode { get; set; }
        public GameInfo Game { get; set; }
        public List<GameInfo> Games { get; set; }
        public int ProtocolVersion { get; set; }
        public int Pid { get; set; }
        public int Session { get; set; }
        public bool Final { get; set; }
    }

    // One run of a game, from Playnite reporting it started until Playnite
    // reports it stopped.
    internal class GameSession
    {
        public int Number;
        public int Pid;
        public string Exe;
        public readonly ManualResetEventSlim Stopped = new ManualResetEventSlim(false);
    }

    public class ZaparooPlugin : GenericPlugin
    {
        private const string PipeName = "zaparoo-playnite-ipc";
        private const string PluginVersion = "1.0.0";
        private const int ProtocolVersion = 1;
        private const int GamesChunkSize = 200;
        private const int ReconnectDelayMs = 3000;

        // How long a game gets to close after being asked before its process
        // tree is ended, and how long Playnite then gets to notice.
        private const int GracefulStopMs = 4000;
        private const int ForcedStopMs = 6000;

        private static readonly ILogger logger = LogManager.GetLogger();
        private static readonly UTF8Encoding utf8 = new UTF8Encoding(false);

        private readonly object writeLock = new object();
        private readonly object sessionLock = new object();
        private readonly Dictionary<Guid, GameSession> sessions = new Dictionary<Guid, GameSession>();
        private readonly CancellationTokenSource shutdown = new CancellationTokenSource();
        private StreamWriter writer;
        private Thread pipeThread;
        private int nextSession;

        public override Guid Id { get; } = Guid.Parse("de439158-de9d-46e7-bab7-ed6142afa0ef");

        public ZaparooPlugin(IPlayniteAPI api) : base(api)
        {
            Properties = new GenericPluginProperties { HasSettings = false };
        }

        public override void OnApplicationStarted(OnApplicationStartedEventArgs args)
        {
            pipeThread = new Thread(PipeLoop) { IsBackground = true, Name = "ZaparooPipe" };
            pipeThread.Start();
        }

        public override void OnApplicationStopped(OnApplicationStoppedEventArgs args)
        {
            shutdown.Cancel();
        }

        public override void OnGameStarted(OnGameStartedEventArgs args)
        {
            var session = new GameSession { Pid = args.StartedProcessId };
            session.Exe = session.Pid > 0 ? ProcessImagePath(session.Pid) : null;
            lock (sessionLock)
            {
                session.Number = ++nextSession;
                sessions[args.Game.Id] = session;
            }
            Send(StartedEvent(args.Game, session));
        }

        public override void OnGameStopped(OnGameStoppedEventArgs args)
        {
            GameSession session;
            lock (sessionLock)
            {
                if (!sessions.TryGetValue(args.Game.Id, out session))
                {
                    return;
                }
                sessions.Remove(args.Game.Id);
            }
            session.Stopped.Set();
            Send(new PluginEvent
            {
                Event = "MediaStopped",
                Id = args.Game.Id.ToString(),
                Session = session.Number,
            });
        }

        public override IEnumerable<GameMenuItem> GetGameMenuItems(GetGameMenuItemsArgs args)
        {
            if (args.Games == null || args.Games.Count != 1)
            {
                yield break;
            }
            var game = args.Games[0];
            yield return new GameMenuItem
            {
                Description = "Write to Zaparoo tag",
                Action = _ =>
                {
                    var sent = Send(new PluginEvent
                    {
                        Event = "Write",
                        Id = game.Id.ToString(),
                        Name = game.Name,
                    });
                    if (!sent)
                    {
                        PlayniteApi.Dialogs.ShowErrorMessage(
                            "Zaparoo Core is not running, so the tag could not be written.", "Zaparoo");
                    }
                },
            };
        }

        private PluginEvent StartedEvent(Game game, GameSession session)
        {
            return new PluginEvent
            {
                Event = "MediaStarted",
                Id = game.Id.ToString(),
                Game = Describe(game, false),
                Pid = session.Pid,
                Exe = session.Exe,
                Session = session.Number,
            };
        }

        // Connects to Core and reads its commands until Playnite closes. Core
        // may start after Playnite or restart while it runs, so the loop keeps
        // retrying for as long as Playnite is open.
        private void PipeLoop()
        {
            var token = shutdown.Token;
            while (!token.IsCancellationRequested)
            {
                try
                {
                    using (var pipe = new NamedPipeClientStream(
                        ".", PipeName, PipeDirection.InOut, PipeOptions.Asynchronous))
                    {
                        pipe.Connect(2000);
                        logger.Info("Connected to Zaparoo Core");
                        Serve(pipe, token);
                    }
                }
                catch (TimeoutException)
                {
                    // Core is not running.
                }
                catch (IOException e)
                {
                    logger.Debug("Zaparoo pipe closed: " + e.Message);
                }
                catch (Exception e) when (!IsCritical(e))
                {
                    // Nothing may escape this thread: an unhandled exception
                    // on it would take Playnite down.
                    logger.Error(e, "Zaparoo pipe failed");
                }
                finally
                {
                    lock (writeLock)
                    {
                        writer = null;
                    }
                }
                token.WaitHandle.WaitOne(ReconnectDelayMs);
            }
        }

        private void Serve(NamedPipeClientStream pipe, CancellationToken token)
        {
            lock (writeLock)
            {
                writer = new StreamWriter(pipe, utf8, 4096, true) { NewLine = "\n", AutoFlush = true };
            }
            using (token.Register(() => ClosePipe(pipe)))
            using (var reader = new StreamReader(pipe, utf8, false, 4096, true))
            {
                Send(new PluginEvent
                {
                    Event = "Hello",
                    PluginVersion = PluginVersion,
                    ProtocolVersion = ProtocolVersion,
                    PlayniteVersion = PlayniteApi.ApplicationInfo.ApplicationVersion.ToString(),
                    Mode = PlayniteApi.ApplicationInfo.Mode.ToString(),
                });
                AnnounceRunningGames();

                string line;
                while ((line = reader.ReadLine()) != null)
                {
                    if (line.Length == 0)
                    {
                        continue;
                    }
                    CoreCommand command;
                    try
                    {
                        command = Serialization.FromJson<CoreCommand>(line);
                    }
                    catch (Exception e) when (!IsCritical(e))
                    {
                        // Playnite's serializer does not document what it
                        // throws for bad input.
                        logger.Warn("Ignoring malformed Zaparoo command: " + e.Message);
                        continue;
                    }
                    if (command == null || string.IsNullOrEmpty(command.Command))
                    {
                        continue;
                    }
                    Dispatch(command);
                }
            }
        }

        // Tells a Core that connected while a game was already running about
        // it, so Core restarting does not lose the game.
        private void AnnounceRunningGames()
        {
            List<KeyValuePair<Guid, GameSession>> running;
            lock (sessionLock)
            {
                running = sessions.ToList();
            }
            foreach (var entry in running)
            {
                var game = PlayniteApi.Database.Games.Get(entry.Key);
                if (game != null)
                {
                    Send(StartedEvent(game, entry.Value));
                }
            }
        }

        // Runs each command off the reader thread, so a slow library request
        // or a stop that waits for a game to close does not hold up the pings
        // and commands behind it.
        private void Dispatch(CoreCommand command)
        {
            switch (command.Command)
            {
                case "Ping":
                    return;
                case "GetGames":
                    ThreadPool.QueueUserWorkItem(_ => Guarded(command, () => SendGames(command)));
                    return;
                case "Launch":
                    ThreadPool.QueueUserWorkItem(_ => Guarded(command, () => Launch(command)));
                    return;
                case "Stop":
                    ThreadPool.QueueUserWorkItem(_ => Guarded(command, () => Stop(command)));
                    return;
                default:
                    SendError(command, "unknown command");
                    return;
            }
        }

        private void Guarded(CoreCommand command, Action action)
        {
            try
            {
                action();
            }
            catch (Exception e) when (!IsCritical(e))
            {
                // Runs on a thread pool thread, where an unhandled exception
                // would take Playnite down.
                logger.Error(e, "Zaparoo command " + command.Command + " failed");
                SendError(command, e.Message);
            }
        }

        private void SendError(CoreCommand command, string message)
        {
            Send(new PluginEvent
            {
                Event = "Error",
                Command = command.Command,
                Id = command.Id,
                RequestId = command.RequestId,
                Error = message,
            });
        }

        private void SendGames(CoreCommand command)
        {
            var chunk = new List<GameInfo>(GamesChunkSize);
            foreach (var game in PlayniteApi.Database.Games)
            {
                chunk.Add(Describe(game, command.Details));
                if (chunk.Count < GamesChunkSize)
                {
                    continue;
                }
                Send(new PluginEvent { Event = "Games", RequestId = command.RequestId, Games = chunk });
                chunk = new List<GameInfo>(GamesChunkSize);
            }
            Send(new PluginEvent { Event = "Games", RequestId = command.RequestId, Games = chunk, Final = true });
        }

        private void Launch(CoreCommand command)
        {
            Guid id;
            if (!Guid.TryParse(command.Id, out id))
            {
                SendError(command, "invalid game ID");
                return;
            }
            var game = PlayniteApi.Database.Games.Get(id);
            if (game == null)
            {
                SendError(command, "game is not in the Playnite library");
                return;
            }
            if (!game.IsInstalled)
            {
                SendError(command, "game is not installed");
                return;
            }

            // StartGame can show Playnite's own dialogs, so it is queued on the
            // UI thread and the result reports only that Playnite has the request.
            PlayniteApi.MainView.UIDispatcher.BeginInvoke((Action)(() =>
            {
                try
                {
                    PlayniteApi.StartGame(id);
                }
                catch (Exception e) when (!IsCritical(e))
                {
                    logger.Error(e, "Failed to start game " + id);
                }
            }));
            Send(new PluginEvent { Event = "LaunchResult", Id = command.Id, Status = "completed" });
        }

        // Ends the running game by closing the process Playnite started for
        // it. The stop only counts once Playnite itself reports the game
        // stopped, which is what Playnite's own tracking decided.
        private void Stop(CoreCommand command)
        {
            Guid id;
            if (!Guid.TryParse(command.Id, out id))
            {
                SendError(command, "invalid game ID");
                return;
            }
            GameSession session;
            lock (sessionLock)
            {
                sessions.TryGetValue(id, out session);
            }
            if (session == null)
            {
                Send(new PluginEvent { Event = "MediaStopResult", Id = command.Id, Status = "completed" });
                return;
            }
            if (session.Pid <= 0 || !ProcessMatches(session))
            {
                Send(new PluginEvent { Event = "MediaStopResult", Id = command.Id, Status = "unsupported" });
                return;
            }

            TaskKill(session.Pid, false);
            if (!session.Stopped.Wait(GracefulStopMs))
            {
                if (ProcessMatches(session))
                {
                    TaskKill(session.Pid, true);
                }
                session.Stopped.Wait(ForcedStopMs);
            }

            if (session.Stopped.IsSet)
            {
                Send(new PluginEvent { Event = "MediaStopResult", Id = command.Id, Status = "completed" });
                return;
            }
            Send(new PluginEvent
            {
                Event = "MediaStopResult",
                Id = command.Id,
                Status = "failed",
                Error = "Playnite still reports the game running",
            });
        }

        // Guards against the PID having been reused by another program since
        // Playnite reported it.
        private static bool ProcessMatches(GameSession session)
        {
            if (string.IsNullOrEmpty(session.Exe))
            {
                return false;
            }
            var current = ProcessImagePath(session.Pid);
            return current != null && string.Equals(current, session.Exe, StringComparison.OrdinalIgnoreCase);
        }

        private static void TaskKill(int pid, bool force)
        {
            var info = new ProcessStartInfo
            {
                FileName = Path.Combine(Environment.SystemDirectory, "taskkill.exe"),
                Arguments = "/PID " + pid + " /T" + (force ? " /F" : ""),
                CreateNoWindow = true,
                UseShellExecute = false,
            };
            using (var process = Process.Start(info))
            {
                process.WaitForExit(5000);
            }
        }

        private GameInfo Describe(Game game, bool details)
        {
            var info = new GameInfo
            {
                Id = game.Id.ToString(),
                Name = game.Name,
                LibraryPluginId = game.PluginId == Guid.Empty ? null : game.PluginId.ToString(),
                IsInstalled = game.IsInstalled,
                Hidden = game.Hidden,
                RomPath = RomPath(game),
            };
            if (game.Platforms != null)
            {
                info.Platforms = game.Platforms
                    .Select(p => new PlatformInfo { SpecificationId = p.SpecificationId, Name = p.Name })
                    .ToList();
            }
            if (!details)
            {
                return info;
            }

            info.Description = game.Description;
            info.ReleaseYear = game.ReleaseYear ?? 0;
            info.Developers = Names(game.Developers);
            info.Publishers = Names(game.Publishers);
            info.Genres = Names(game.Genres);
            info.Cover = ImagePath(game.CoverImage);
            info.Background = ImagePath(game.BackgroundImage);
            info.Icon = ImagePath(game.Icon);
            return info;
        }

        private static List<string> Names<T>(List<T> items) where T : DatabaseObject
        {
            if (items == null || items.Count == 0)
            {
                return null;
            }
            return items.Where(i => !string.IsNullOrWhiteSpace(i.Name)).Select(i => i.Name).ToList();
        }

        private string RomPath(Game game)
        {
            if (game.Roms == null || game.Roms.Count == 0 || string.IsNullOrEmpty(game.Roms[0].Path))
            {
                return null;
            }
            try
            {
                return PlayniteApi.ExpandGameVariables(game, game.Roms[0].Path);
            }
            catch (Exception e) when (!IsCritical(e))
            {
                logger.Debug("Could not expand ROM path for " + game.Name + ": " + e.Message);
                return null;
            }
        }

        // Resolves a library image to a file on disk. Images Playnite has not
        // downloaded are web addresses, which Core has no use for.
        private string ImagePath(string databasePath)
        {
            if (string.IsNullOrEmpty(databasePath) ||
                databasePath.StartsWith("http", StringComparison.OrdinalIgnoreCase))
            {
                return null;
            }
            try
            {
                var path = PlayniteApi.Database.GetFullFilePath(databasePath);
                return File.Exists(path) ? path : null;
            }
            catch (Exception e) when (!IsCritical(e))
            {
                logger.Debug("Could not resolve image " + databasePath + ": " + e.Message);
                return null;
            }
        }

        private bool Send(PluginEvent message)
        {
            string json;
            try
            {
                json = Serialization.ToJson(message);
            }
            catch (Exception e) when (!IsCritical(e))
            {
                logger.Error(e, "Failed to encode Zaparoo event " + message.Event);
                return false;
            }
            lock (writeLock)
            {
                if (writer == null)
                {
                    return false;
                }
                try
                {
                    writer.WriteLine(json);
                    return true;
                }
                catch (Exception e) when (e is IOException || e is ObjectDisposedException ||
                    e is InvalidOperationException)
                {
                    logger.Debug("Zaparoo pipe write failed: " + e.Message);
                    writer = null;
                    return false;
                }
            }
        }

        // Closes the pipe to unblock the reader when Playnite shuts down.
        private static void ClosePipe(NamedPipeClientStream pipe)
        {
            try
            {
                pipe.Dispose();
            }
            catch (Exception e) when (e is IOException || e is ObjectDisposedException)
            {
                logger.Debug("Zaparoo pipe close failed: " + e.Message);
            }
        }

        // Reports exceptions that say the process itself is no longer sound.
        // Those are never handled here; everything else is contained so a
        // fault in this extension cannot take Playnite down with it.
        private static bool IsCritical(Exception e)
        {
            return e is OutOfMemoryException || e is StackOverflowException ||
                e is AccessViolationException || e is ThreadAbortException;
        }

        private const uint ProcessQueryLimitedInformation = 0x1000;

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern IntPtr OpenProcess(uint access, bool inherit, int pid);

        [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern bool QueryFullProcessImageName(
            IntPtr process, uint flags, StringBuilder name, ref uint size);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool CloseHandle(IntPtr handle);

        // Reads a process's image path. Process.MainModule cannot be used:
        // Playnite is a 32-bit program and most games are 64-bit.
        private static string ProcessImagePath(int pid)
        {
            var handle = OpenProcess(ProcessQueryLimitedInformation, false, pid);
            if (handle == IntPtr.Zero)
            {
                return null;
            }
            try
            {
                var buffer = new StringBuilder(32768);
                var size = (uint)buffer.Capacity;
                return QueryFullProcessImageName(handle, 0, buffer, ref size) ? buffer.ToString() : null;
            }
            finally
            {
                CloseHandle(handle);
            }
        }
    }
}
