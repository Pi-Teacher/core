import React from 'react';

interface SidebarProps {
  currentPath: string;
  onNavigate?: (path: string) => void;
  badges?: {
    review?: number;
    cards?: number;
    topics?: number;
    glossary?: number;
    approvals?: number;
  };
}

export const Sidebar: React.FC<SidebarProps> = ({
  currentPath = 'review',
  onNavigate,
  badges = {
    review: 28,
    cards: 412,
    topics: 14,
    glossary: 86,
    approvals: 3
  }
}) => {
  const navItems = [
    {
      path: 'dashboard',
      label: '首页仪表盘',
      icon: 'space_dashboard'
    },
    {
      path: 'review',
      label: '卡片复习',
      icon: 'repeat',
      badge: badges.review,
      badgeColor: 'bg-tertiary-fixed text-on-tertiary-fixed font-bold'
    },
    {
      path: 'cards',
      label: '卡片管理',
      icon: 'style',
      badge: badges.cards,
      badgeColor: 'bg-surface-container-high text-on-surface-variant font-medium'
    },
    {
      path: 'topics',
      label: '知识分类',
      icon: 'folder_special',
      badge: badges.topics,
      badgeColor: 'bg-surface-container-high text-on-surface-variant font-medium'
    },
    {
      path: 'glossary',
      label: '术语表',
      icon: 'menu_book',
      badge: badges.glossary,
      badgeColor: 'bg-surface-container-high text-on-surface-variant font-medium'
    },
    {
      path: 'approvals',
      label: 'AI 审批中心',
      icon: 'smart_toy',
      badge: badges.approvals ? `${badges.approvals} 待审批` : undefined,
      badgeColor: 'bg-secondary-fixed text-on-secondary-fixed font-bold'
    }
  ];

  const secondaryNavItems = [
    {
      path: 'trash',
      label: '回收站',
      icon: 'delete',
      hoverIconColor: 'group-hover:text-error'
    },
    {
      path: 'profile',
      label: '个人信息与偏好',
      icon: 'manage_accounts'
    },
    {
      path: 'settings',
      label: '系统设置',
      icon: 'settings'
    }
  ];

  return (
    <aside className="fixed left-0 top-0 h-full w-72 bg-surface-container-lowest z-50 flex flex-col shadow-[0_1px_8px_rgba(0,0,0,0.04)] border-r border-outline-variant/30">
      {/* Brand Header */}
      <div className="h-16 px-space-md flex items-center gap-space-sm bg-surface-container-lowest border-b border-outline-variant/20">
        <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center text-on-primary shadow-sm">
          <span className="material-symbols-outlined text-[20px]">psychology</span>
        </div>
        <div className="flex flex-col min-w-0">
          <span className="font-headline-sm text-[16px] leading-tight text-on-surface font-semibold tracking-tight truncate">
            Pi Teacher
          </span>
          <span className="font-label-sm text-[11px] text-on-surface-variant truncate font-mono">
            FSRS Spaced Repetition
          </span>
        </div>
      </div>

      {/* Main Navigation */}
      <div className="flex-1 overflow-y-auto px-space-sm py-space-sm">
        <nav className="flex flex-col gap-1.5 p-1">
          {navItems.map((item) => {
            const isActive = currentPath === item.path;
            return (
              <button
                key={item.path}
                onClick={() => onNavigate?.(item.path)}
                className={`group relative flex items-center justify-between px-3.5 py-2.5 rounded-xl transition-all duration-150 text-left w-full ${
                  isActive
                    ? 'bg-primary-container text-on-primary font-semibold shadow-sm'
                    : 'text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high'
                }`}
              >
                <div className="flex items-center gap-3">
                  <span
                    className={`material-symbols-outlined text-[20px] transition-colors ${
                      isActive
                        ? 'text-on-primary'
                        : 'text-on-surface-variant group-hover:text-primary'
                    }`}
                  >
                    {item.icon}
                  </span>
                  <span className="font-body-md text-[14px] tracking-tight">
                    {item.label}
                  </span>
                </div>
                {item.badge !== undefined && (
                  <span
                    className={`font-label-sm text-[11px] px-2 py-0.5 rounded-full font-mono ${
                      isActive ? 'bg-surface-bright/20 text-on-primary' : item.badgeColor
                    }`}
                  >
                    {item.badge}
                  </span>
                )}
                {isActive && item.badge === undefined && (
                  <span className="w-1.5 h-1.5 rounded-full bg-surface-bright" />
                )}
              </button>
            );
          })}

          <div className="my-2 mx-2 h-[1px] bg-surface-container-high" />

          {secondaryNavItems.map((item) => {
            const isActive = currentPath === item.path;
            return (
              <button
                key={item.path}
                onClick={() => onNavigate?.(item.path)}
                className={`group relative flex items-center justify-between px-3.5 py-2.5 rounded-xl transition-all duration-150 text-left w-full ${
                  isActive
                    ? 'bg-primary-container text-on-primary font-semibold shadow-sm'
                    : 'text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high'
                }`}
              >
                <div className="flex items-center gap-3">
                  <span
                    className={`material-symbols-outlined text-[20px] transition-colors ${
                      isActive
                        ? 'text-on-primary'
                        : item.hoverIconColor || 'text-on-surface-variant group-hover:text-primary'
                    }`}
                  >
                    {item.icon}
                  </span>
                  <span className="font-body-md text-[14px] tracking-tight">
                    {item.label}
                  </span>
                </div>
                {isActive && <span className="w-1.5 h-1.5 rounded-full bg-surface-bright" />}
              </button>
            );
          })}
        </nav>
      </div>

      {/* User Info Bottom Dock */}
      <div className="p-space-sm flex flex-col gap-space-sm bg-surface-container-low border-t border-outline-variant/30">
        <div className="flex items-center justify-between p-space-xs">
          <div className="flex items-center gap-space-sm min-w-0">
            <div className="w-8 h-8 rounded-full bg-primary/10 border border-primary/20 flex items-center justify-center font-headline-sm text-headline-sm text-primary font-bold">
              X
            </div>
            <div className="flex flex-col min-w-0">
              <span className="font-body-sm text-[13px] font-semibold text-on-surface truncate">
                Xyzen
              </span>
              <span className="font-label-sm text-[11px] text-on-surface-variant truncate font-mono">
                xyzen@pi-teacher.local
              </span>
            </div>
          </div>
          <button
            title="快捷设置"
            onClick={() => onNavigate?.('settings')}
            className="text-on-surface-variant hover:text-on-surface p-1 rounded-lg hover:bg-surface-container transition-colors"
          >
            <span className="material-symbols-outlined text-[18px]">more_vert</span>
          </button>
        </div>
      </div>
    </aside>
  );
};
