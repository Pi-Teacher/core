import React from 'react';

interface MarkdownRendererProps {
  content: string;
  className?: string;
}

/**
 * 轻量 Markdown 渲染器.
 * 真实实现计划使用 react-markdown + remark-gfm + rehype-sanitize + rehype-highlight,
 * 见 程序描述.md 第 12 节 (仓库根目录). 原型阶段用最小实现覆盖标题/列表/引用/表格/代码块/行内代码.
 */

const renderInline = (text: string): React.ReactNode => {
  // 先切行内代码
  const parts = text.split(/(`[^`]+`)/g);
  return parts.map((part, i) => {
    if (part.startsWith('`') && part.endsWith('`')) {
      return (
        <code
          key={i}
          className="px-1.5 py-0.5 rounded bg-surface-container-high text-primary font-mono text-[13px] border border-outline-variant/40"
        >
          {part.slice(1, -1)}
        </code>
      );
    }
    // 再切加粗
    const bold = part.split(/(\*\*[^*]+\*\*)/g);
    return (
      <React.Fragment key={i}>
        {bold.map((b, j) =>
          b.startsWith('**') && b.endsWith('**') ? (
            <strong key={j} className="font-semibold text-on-surface">
              {b.slice(2, -2)}
            </strong>
          ) : (
            b
          )
        )}
      </React.Fragment>
    );
  });
};

const isTableSeparator = (line: string) =>
  /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(line);

const splitRow = (line: string) =>
  line
    .trim()
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split('|')
    .map((c) => c.trim());

export const MarkdownRenderer: React.FC<MarkdownRendererProps> = ({
  content,
  className = ''
}) => {
  const lines = content.split('\n');
  const out: React.ReactNode[] = [];
  let inCode = false;
  let codeLines: string[] = [];
  let codeLang = '';

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // 代码块
    if (line.trim().startsWith('```')) {
      if (inCode) {
        out.push(
          <div
            key={`code-${i}`}
            className="my-4 rounded-lg overflow-hidden border border-slate-700/80 bg-[#0F172A]"
          >
            <div className="flex items-center justify-between px-4 py-1.5 bg-slate-800/80 border-b border-slate-700/60 text-slate-400 font-mono text-[11px]">
              <span className="uppercase tracking-wider">{codeLang || 'code'}</span>
              <span>syntax</span>
            </div>
            <pre className="p-4 text-slate-100 font-mono text-[13px] leading-[20px] overflow-x-auto whitespace-pre">
              <code>{codeLines.join('\n')}</code>
            </pre>
          </div>
        );
        inCode = false;
        codeLines = [];
        codeLang = '';
      } else {
        inCode = true;
        codeLang = line.trim().replace(/^```/, '');
        codeLines = [];
      }
      continue;
    }

    if (inCode) {
      codeLines.push(line);
      continue;
    }

    // 表格
    if (line.trim().startsWith('|') && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
      const header = splitRow(line);
      const rows: string[][] = [];
      let j = i + 2;
      while (j < lines.length && lines[j].trim().startsWith('|')) {
        rows.push(splitRow(lines[j]));
        j++;
      }
      out.push(
        <div key={`table-${i}`} className="my-4 overflow-x-auto rounded-lg border border-outline-variant/50">
          <table className="w-full border-collapse text-[13px]">
            <thead>
              <tr className="bg-surface-container-low">
                {header.map((h, hi) => (
                  <th
                    key={hi}
                    className="px-3 py-2 text-left font-semibold text-on-surface border-b border-outline-variant/50 whitespace-nowrap"
                  >
                    {renderInline(h)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, ri) => (
                <tr key={ri} className="even:bg-surface-container-lowest odd:bg-surface/60">
                  {row.map((cell, ci) => (
                    <td
                      key={ci}
                      className="px-3 py-2 align-top text-on-surface border-b border-outline-variant/20"
                    >
                      {renderInline(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      i = j - 1;
      continue;
    }

    // 标题
    if (line.startsWith('### ')) {
      out.push(
        <h3 key={i} className="font-headline-sm text-headline-sm text-on-surface mt-4 mb-2">
          {renderInline(line.slice(4))}
        </h3>
      );
    } else if (line.startsWith('## ')) {
      out.push(
        <h2
          key={i}
          className="font-headline-md text-headline-md text-on-surface mt-5 mb-2.5 pb-1.5 border-b border-outline-variant/30"
        >
          {renderInline(line.slice(3))}
        </h2>
      );
    } else if (line.startsWith('# ')) {
      out.push(
        <h1 key={i} className="font-headline-lg text-headline-lg text-on-surface mt-6 mb-3">
          {renderInline(line.slice(2))}
        </h1>
      );
    } else if (line.startsWith('- ')) {
      out.push(
        <li key={i} className="ml-5 list-disc text-body-md text-on-surface my-1">
          {renderInline(line.slice(2))}
        </li>
      );
    } else if (line.startsWith('> ')) {
      out.push(
        <blockquote
          key={i}
          className="my-3 pl-3.5 py-1.5 border-l-2 border-primary-container bg-primary-container/5 text-on-surface-variant italic rounded-r-lg"
        >
          {renderInline(line.slice(2))}
        </blockquote>
      );
    } else if (line.trim() === '') {
      out.push(<div key={i} className="h-2" />);
    } else {
      out.push(
        <p key={i} className="text-body-md text-on-surface my-1">
          {renderInline(line)}
        </p>
      );
    }
  }

  return <div className={className}>{out}</div>;
};
