import { createFileRoute, Link } from '@tanstack/react-router'
import { api } from '~/api/client'

export const Route = createFileRoute('/')({
  loader: async () => {
    const { data } = await api().GET('/api/me')
    return { me: data ?? null }
  },
  component: Home,
})

function Home() {
  const { me } = Route.useLoaderData()
  return (
    <main>
      <h1>Open Communities</h1>
      {me ? (
        <p>Signed in as {me.displayName || me.name}.</p>
      ) : (
        <p>
          <Link to="/login">Sign in or create an account</Link>
        </p>
      )}
    </main>
  )
}
