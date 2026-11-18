import { useTranslation } from 'react-i18next'
import { DISCLAIMER } from './disclaimer'

// Full legal text, verbatim from the spec, in the reader's language (01 §Vị trí).
export function AboutPage() {
  const { i18n } = useTranslation()
  const d = DISCLAIMER[i18n.language === 'vi' ? 'vi' : 'en']
  return (
    <div className="page">
      <div className="page-head on-sun">
        <h1>{d.title}</h1>
      </div>
      <article className="cel prose">
        {d.sections.map((s) => (
          <section key={s.n}>
            <p>
              <strong>
                {s.n}. {s.title}
              </strong>{' '}
              {s.title.startsWith('Contact') || s.title.startsWith('Liên hệ') ? <a href={`mailto:${s.body}`}>{s.body}</a> : s.body}
            </p>
            {s.bullets.length > 0 && (
              <ul>
                {s.bullets.map((b) => (
                  <li key={b}>{b}</li>
                ))}
              </ul>
            )}
          </section>
        ))}
      </article>
    </div>
  )
}
