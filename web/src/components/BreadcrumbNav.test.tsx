import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import BreadcrumbNav from './BreadcrumbNav'

describe('BreadcrumbNav', () => {
  it('renders all breadcrumb items', () => {
    const items = [
      { title: 'Home' },
      { title: 'Documents' },
      { title: 'photo.jpg' },
    ]
    render(<BreadcrumbNav items={items} />)
    expect(screen.getByText('Home')).toBeInTheDocument()
    expect(screen.getByText('Documents')).toBeInTheDocument()
    expect(screen.getByText('photo.jpg')).toBeInTheDocument()
  })

  it('renders empty without crashing', () => {
    expect(() => render(<BreadcrumbNav items={[]} />)).not.toThrow()
  })

  it('renders single item', () => {
    render(<BreadcrumbNav items={[{ title: 'root' }]} />)
    expect(screen.getByText('root')).toBeInTheDocument()
  })
})