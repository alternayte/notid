import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { meOptions } from '~/api/gen/@tanstack/react-query.gen'

export const Route = createFileRoute('/')({
  // A signed-out visitor gets 401; the page then shows the sign-in link.
  loader: ({ context }) => context.queryClient.prefetchQuery({ ...meOptions(), retry: false }),
  component: Home,
})

function Home() {
  const { data: me } = useQuery({ ...meOptions(), retry: false })
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
