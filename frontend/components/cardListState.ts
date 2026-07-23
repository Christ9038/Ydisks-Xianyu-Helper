import type { Card } from '../types';

export const filterCards = (
  cards: Card[],
  typeFilter: Card['type'] | '',
  nameSearch: string,
): Card[] => {
  const keyword = nameSearch.trim().toLocaleLowerCase();
  return cards.filter(card => (
    (!typeFilter || card.type === typeFilter)
    && (!keyword || card.name.toLocaleLowerCase().includes(keyword))
  ));
};
