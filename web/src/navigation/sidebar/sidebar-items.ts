import {
  Activity,
  Ban,
  BellRing,
  Bot,
  Brain,
  Bug,
  ClipboardCheck,
  ClipboardList,
  FolderOpen,
  FolderSync,
  LayoutDashboard,
  type LucideIcon,
  MessageSquare,
  Network,
  Plug,
  Radio,
  ScrollText,
  Settings2,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Target,
  Terminal,
  Wrench,
} from "lucide-react";

export type NavBadge = "new" | "soon";

export interface NavSubItem {
  id: string;
  title: string;
  url: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

interface NavItemBase {
  id: string;
  title: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

export interface NavMainLinkItem extends NavItemBase {
  url: string;
  subItems?: never;
}

export interface NavMainParentItem extends NavItemBase {
  subItems: NavSubItem[];
}

export type NavMainItem = NavMainLinkItem | NavMainParentItem;

export interface NavGroup {
  id: number;
  label?: string;
  items: NavMainItem[];
}

const systemItems: NavMainItem[] = [
  { id: "llm", title: "LLM", url: "/system/llm", icon: Brain },
  { id: "agents", title: "Agent", url: "/system/agents", icon: Bot },
  { id: "mcp", title: "MCP", url: "/system/mcp", icon: Plug },
  { id: "skills", title: "Skill", url: "/system/skills", icon: Sparkles },
  { id: "tools", title: "Tools", url: "/system/tools", icon: Wrench },
  { id: "notify", title: "Notifications", url: "/system/notify", icon: BellRing },
  { id: "intercept", title: "Intercept rules", url: "/system/intercept", icon: ShieldAlert },
  { id: "asset-intercept", title: "Asset intercept", url: "/system/intercept/assets", icon: Ban },
  { id: "approvals", title: "Approval records", url: "/system/intercept/approvals", icon: ClipboardList },
  { id: "logs", title: "Logs", url: "/system/logs", icon: ScrollText },
  { id: "settings", title: "System settings", url: "/system/settings", icon: Settings2 },
];

export const redSidebarItems: NavGroup[] = [
  {
    id: 1,
    label: "Features",
    items: [
      { id: "dashboard", title: "Dashboard", url: "/redteam/dashboard", icon: LayoutDashboard },
      { id: "chat", title: "Chat", url: "/redteam/chat", icon: MessageSquare },
      { id: "tasks", title: "Tasks", url: "/redteam/tasks", icon: Target },
      { id: "findings", title: "Findings", url: "/redteam/findings", icon: Bug },
      { id: "traffic", title: "Traffic", url: "/redteam/traffic", icon: Activity },
      { id: "commands", title: "Tool executions", url: "/redteam/commands", icon: Terminal },
      { id: "llm-records", title: "LLM records", url: "/redteam/llm-records", icon: Radio },
      { id: "assets", title: "Assets", url: "/redteam/assets", icon: Network },
      { id: "sync", title: "Asset sync", url: "/redteam/sync", icon: FolderSync },
      { id: "workspace", title: "Workspace", url: "/redteam/workspace", icon: FolderOpen },
    ],
  },
  {
    id: 2,
    label: "System",
    items: systemItems,
  },
];

export const blueSidebarItems: NavGroup[] = [
  {
    id: 1,
    label: "Features",
    items: [
      { id: "blueDashboard", title: "Dashboard", url: "/blue/dashboard", icon: LayoutDashboard },
      { id: "chat", title: "Chat", url: "/blue/chat", icon: MessageSquare },
      { id: "tasks", title: "Tasks", url: "/blue/tasks", icon: Target },
      { id: "blueCases", title: "Cases", url: "/blue/cases", icon: FolderOpen },
      {
        id: "blueDetection",
        title: "Detection",
        icon: ShieldCheck,
        subItems: [
          { id: "blueAlerts", title: "Alerts", url: "/blue/alerts", icon: BellRing },
          { id: "blueRules", title: "Detection rules", url: "/blue/rules", icon: ShieldCheck },
        ],
      },
      { id: "blueEvents", title: "Events", url: "/blue/events", icon: ScrollText },
      { id: "blueActions", title: "Response actions", url: "/blue/actions", icon: ClipboardCheck },
      { id: "commands", title: "Tool executions", url: "/blue/commands", icon: Terminal },
      { id: "llm-records", title: "LLM records", url: "/blue/llm-records", icon: Radio },
      { id: "assets", title: "Assets", url: "/blue/assets", icon: Network },
      { id: "sync", title: "Asset sync", url: "/blue/sync", icon: FolderSync },
      { id: "workspace", title: "Workspace", url: "/blue/workspace", icon: FolderOpen },
    ],
  },
  {
    id: 2,
    label: "System",
    items: systemItems,
  },
];

// Keep the original export for consumers that still render the red-team menu.
export const sidebarItems = redSidebarItems;
